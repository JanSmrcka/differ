package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/testutil"
	"github.com/jansmrcka/differ/internal/theme"
)

// Trying a theme meant editing a config file or restarting with --theme, which
// is a poor way to find out whether you like one.

func pickerModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}}})
	m.theme = theme.Themes["mocha"]
	m.styles = NewStyles(m.theme)
	return m
}

func TestThemePicker_OpensListingEveryThemeWithTheCurrentOneMarked(t *testing.T) {
	t.Parallel()
	// The picker opens on the theme actually in effect, which is not
	// necessarily the one in the config — --theme overrides it.
	m := pickerModel(t)
	m.cfg.Theme = "gruvbox"
	m.theme = theme.Themes["gruvbox"]
	m.styles = NewStyles(m.theme)

	updated, _ := m.routeKey(key("t"))
	m = updated.(Model)
	if !m.showThemes {
		t.Fatal("t did not open the picker")
	}

	got := stripANSI(m.renderThemeOverlay(80, 16))
	for _, name := range theme.ThemeNames() {
		if !strings.Contains(got, name) {
			t.Errorf("the picker does not offer %q:\n%s", name, got)
		}
	}
	// It opens on whatever is in use, not at the top of the list.
	if theme.ThemeNames()[m.themeCursor] != "gruvbox" {
		t.Errorf("the cursor starts on %q, want the theme in use",
			theme.ThemeNames()[m.themeCursor])
	}
}

// Moving the selection changes the colours there and then — that is the point
// of a picker over editing a config file.
func TestThemePicker_MovingTheSelectionPreviewsIt(t *testing.T) {
	t.Parallel()
	m := pickerModel(t)
	updated, _ := m.routeKey(key("t"))
	m = updated.(Model)
	before := m.theme

	moved, cmd := m.routeKey(key("j"))
	m = moved.(Model)
	if m.theme == before {
		t.Error("moving the selection did not change the theme")
	}
	if m.theme != theme.Themes[theme.ThemeNames()[m.themeCursor]] {
		t.Error("the preview does not match the selection")
	}
	// Not m.styles.DiffAdded.String(): that is Render(""), which is empty for
	// every style under the test profile. What is observable is that the
	// syntax highlighting followed, and that a re-render was scheduled —
	// DiffRenderer bakes the styles into each cached row, so recolouring is
	// not enough.
	if got := currentChromaStyleName(); got != m.theme.ChromaStyle {
		t.Errorf("the Chroma style is %q, want the previewed theme's %q", got, m.theme.ChromaStyle)
	}
	if cmd == nil {
		t.Error("no re-render was scheduled for the previewed theme")
	}
}

// Cancelling puts back exactly what was there and writes nothing.
func TestThemePicker_EscRestoresTheThemeAndSavesNothing(t *testing.T) {
	t.Parallel()
	// liveModel, not pickerModel: this test runs the command it gets back, and
	// cancelling schedules a re-render that talks to git.
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "init")
	tr.Modify("a.ts", "two\n")
	m := liveModel(t, tr)
	m.theme = theme.Themes["mocha"]
	m.styles = NewStyles(m.theme)
	before := m.theme

	updated, _ := m.routeKey(key("t"))
	m = updated.(Model)
	for range 2 {
		moved, _ := m.routeKey(key("j"))
		m = moved.(Model)
	}
	if m.theme == before {
		t.Fatal("the preview never changed")
	}

	closed, cmd := m.routeKey(tea.KeyMsg{Type: tea.KeyEsc})
	got := closed.(Model)
	if got.showThemes {
		t.Error("esc did not close the picker")
	}
	if got.theme != before {
		t.Error("esc did not put the original theme back")
	}
	if got.cfg.Theme == theme.ThemeNames()[got.themeCursor] && got.cfg.Theme != "" {
		t.Error("esc recorded the previewed theme in the config")
	}
	// A re-render is expected — the original colours have to go back on the
	// screen. A config write is not.
	if cmd != nil {
		if _, saved := cmd().(savePrefDoneMsg); saved {
			t.Error("esc saved the config; cancelling must write nothing")
		}
	}
}

// Confirming keeps the theme and remembers it for next time.
func TestThemePicker_EnterKeepsTheThemeAndPersistsIt(t *testing.T) {
	t.Parallel()
	m := pickerModel(t)
	updated, _ := m.routeKey(key("t"))
	m = updated.(Model)
	moved, _ := m.routeKey(key("j"))
	m = moved.(Model)

	chosen := theme.ThemeNames()[m.themeCursor]
	confirmed, cmd := m.routeKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := confirmed.(Model)

	if got.showThemes {
		t.Error("enter did not close the picker")
	}
	if got.theme != theme.Themes[chosen] {
		t.Error("enter did not keep the previewed theme")
	}
	if got.cfg.Theme != chosen {
		t.Errorf("cfg.Theme = %q, want %q", got.cfg.Theme, chosen)
	}
	if cmd == nil {
		t.Error("enter did not save the choice")
	}
}

// The syntax highlighting has to follow the theme, which it could not: the
// Chroma style was cached behind a sync.Once, so the first theme of the
// session was the only one that ever took effect.
func TestThemePicker_TheSyntaxHighlightingFollowsTheTheme(t *testing.T) {
	t.Parallel()
	initChromaStyle("catppuccin-mocha")
	first := currentChromaStyleName()

	initChromaStyle("gruvbox")
	if got := currentChromaStyleName(); got == first {
		t.Errorf("the Chroma style is still %q after switching themes", got)
	}
	if got := currentChromaStyleName(); got != "gruvbox" {
		t.Errorf("the Chroma style is %q, want gruvbox", got)
	}
}
