package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/feedback"
	"github.com/jansmrcka/differ/internal/testutil"
)

// barModel is a real repo in a given mode at a given width.
func barModel(t *testing.T, width int) Model {
	t.Helper()
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	u, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 32})
	return u.(Model)
}

// The old hint bar used Width(), which wraps — so a long list silently became
// two rows and pushed the top of the layout off screen.
func TestCommandBar_StaysOnOneLineAtEveryWidth(t *testing.T) {
	for _, width := range []int{80, 100, 120, 200} {
		m := barModel(t, width)
		for _, mode := range []viewMode{modeFileList, modeDiff, modeReview} {
			m.mode = mode
			bar := m.renderCommandBar()
			if h := lipgloss.Height(bar); h != 1 {
				t.Errorf("width %d mode %v: bar is %d rows:\n%s", width, mode, h, bar)
			}
			if w := lipgloss.Width(bar); w > width {
				t.Errorf("width %d mode %v: bar is %d columns wide", width, mode, w)
			}
		}
	}
}

// Truncating must never drop the two keys that get the user out.
func TestCommandBar_KeepsHelpAndQuitWhenItHasToTruncate(t *testing.T) {
	m := barModel(t, 40)
	m.mode = modeReview
	bar := m.renderCommandBar()

	for _, want := range []string{"?", "q"} {
		if !strings.Contains(bar, want) {
			t.Errorf("a narrow bar dropped %q:\n%s", want, bar)
		}
	}
}

// Each view offers its own commands, not one shared list.
func TestCommandBar_IsContextual(t *testing.T) {
	m := barModel(t, 120)

	m.mode = modeFileList
	list := m.renderCommandBar()
	m.mode = modeDiff
	diff := m.renderCommandBar()

	if list == diff {
		t.Error("the file list and the diff show the same commands")
	}
	if !strings.Contains(list, "open") {
		t.Errorf("file list bar has no open:\n%s", list)
	}
	if !strings.Contains(diff, "hunk") {
		t.Errorf("diff bar has no hunk navigation:\n%s", diff)
	}
}

// State-dependent commands appear only when they would do something.
func TestCommandBar_SendAppearsOnlyWithSomethingToSend(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	u, _ := m.updateFileListMode(key("r"))
	m = u.(Model)

	if bar := m.renderCommandBar(); strings.Contains(bar, "send") {
		t.Errorf("send offered with no comments:\n%s", bar)
	}

	m.target = feedback.NewFake()
	m = commentAt(t, m, LineAdded, "  const user = await getUser(id)", "look at this")

	if bar := m.renderCommandBar(); !strings.Contains(bar, "send") {
		t.Errorf("send not offered with a pending comment:\n%s", bar)
	}
}

// ? opens a full list of the current view's keys, and closes again.
func TestHelpOverlay_OpensAndCloses(t *testing.T) {
	m := barModel(t, 120)
	m.mode = modeFileList

	u, _ := m.Update(key("?"))
	m = u.(Model)
	if !m.showHelp {
		t.Fatal("? did not open the help overlay")
	}

	view := m.View()
	// The overlay lists things the compact bar leaves out.
	for _, want := range []string{"stage every change", "push to the upstream branch"} {
		if !strings.Contains(view, want) {
			t.Errorf("the overlay does not explain %q:\n%s", want, view)
		}
	}

	u, _ = m.Update(key("?"))
	if u.(Model).showHelp {
		t.Error("? did not close the overlay again")
	}

	m.showHelp = true
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if u.(Model).showHelp {
		t.Error("esc did not close the overlay")
	}
}

// The overlay must not change the layout's height, or the diff viewport
// resizes when it opens and does not come back.
func TestHelpOverlay_DoesNotResizeTheLayout(t *testing.T) {
	m := barModel(t, 120)
	m.mode = modeDiff
	before := lipgloss.Height(m.View())

	m.showHelp = true
	if after := lipgloss.Height(m.View()); after != before {
		t.Errorf("view height went %d → %d with the overlay open", before, after)
	}
}

// Whatever a mode's keymap says is in the bar has to be reachable from the
// bar's own text, or the two have drifted.
func TestCommandBar_ShowsWhatTheKeymapMarksForIt(t *testing.T) {
	m := barModel(t, 200)
	for _, mode := range []viewMode{modeFileList, modeDiff, modeReview} {
		m.mode = mode
		bar := m.renderCommandBar()
		for _, b := range keymapFor(mode) {
			if !b.Bar || b.Desc == "" {
				continue
			}
			// send is state-dependent and covered by its own test.
			if b.Desc == "send" {
				continue
			}
			if !strings.Contains(bar, b.Desc) {
				t.Errorf("mode %v: bar omits %q (%s):\n%s", mode, b.Desc, b.label(), bar)
			}
		}
	}
}

// The overlay swallowed every key it did not handle, ctrl+c included — while
// printing "quit immediately" next to it.
func TestHelpOverlay_CtrlCStillQuits(t *testing.T) {
	m := barModel(t, 120)
	m.mode = modeFileList
	m.showHelp = true

	// Through Update, because ctrl+c is answered in the dispatcher now,
	// before any mode or overlay can claim it — that is what makes it work in
	// the commit input too.
	//
	// Built by hand, from when key() silently turned any multi-rune name into
	// KeyDown — which is how this bug was first "confirmed" while actually
	// sending the wrong key. key("ctrl+c") is correct now, but the explicit
	// form is what this test is about.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c did nothing with the overlay open")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c should quit with the overlay open")
	}
}

// Where keys go into an input, ? and q are characters, not commands — so the
// bar must not offer them.
func TestCommandBar_DoesNotOfferKeysThatGoIntoAnInput(t *testing.T) {
	m := barModel(t, 120)
	m.mode = modeBranchPicker

	bar := m.renderCommandBar()
	for _, unwanted := range []string{"help", "quit"} {
		if strings.Contains(bar, unwanted) {
			t.Errorf("the branch picker bar offers %q, which types into the filter:\n%s", unwanted, bar)
		}
	}
	// It still has to say how to get out.
	if !strings.Contains(bar, "close") {
		t.Errorf("the branch picker bar offers no way out:\n%s", bar)
	}
}

// The bar hides commands that would do nothing; the overlay was still listing
// them.
func TestHelpOverlay_HidesCommandsThatWouldDoNothing(t *testing.T) {
	m := barModel(t, 120)
	m.mode = modeFileList
	m.stagedOnly = true

	overlay := m.renderHelpOverlay(m.width, 30)
	for _, unwanted := range []string{"stage every change", "stage or unstage"} {
		if strings.Contains(overlay, unwanted) {
			t.Errorf("the overlay offers %q while looking at the index:\n%s", unwanted, overlay)
		}
	}
}

// A bar with nothing pinned renders nothing once the terminal is narrow
// enough, which is how the first fix for the branch picker left it.
func TestCommandBar_NeverRendersEmpty(t *testing.T) {
	for _, width := range []int{40, 50, 60, 80} {
		m := barModel(t, width)
		for _, mode := range []viewMode{modeFileList, modeDiff, modeReview, modeBranchPicker, modeCommit} {
			m.mode = mode
			if bar := strings.TrimSpace(stripANSI(m.renderCommandBar())); bar == "" {
				t.Errorf("width %d mode %v: the bar is empty", width, mode)
			}
		}
	}
}
