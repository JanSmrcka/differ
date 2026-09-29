package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
	"github.com/jansmrcka/differ/internal/theme"
)

// The layout was fixed: 35 columns of file list whatever the terminal, and a
// "terminal too small" wall below 60x10. In a narrow tmux split that meant the
// diff — the thing differ is for — got whatever was left.

func responsiveModel(t *testing.T, w, h int) Model {
	t.Helper()
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	m := liveModel(t, tr)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = updated.(Model)
	if cmd := m.loadDiffCmd(false); cmd != nil {
		u, _ := m.Update(cmd())
		m = u.(Model)
	}
	return m
}

// The file list takes a share of the terminal rather than a fixed 35 columns,
// so a narrow terminal spends its width on the diff.
func TestResponsive_TheFileListScalesWithTheTerminal(t *testing.T) {
	t.Parallel()
	var last int
	for _, w := range []int{80, 100, 120, 160, 200} {
		m := responsiveModel(t, w, 30)
		got := m.listWidth()

		if got < minListWidth || got > maxListWidth {
			t.Errorf("width %d: file list is %d columns, outside [%d, %d]",
				w, got, minListWidth, maxListWidth)
		}
		if got < last {
			t.Errorf("width %d: file list shrank to %d from %d at a narrower terminal", w, got, last)
		}
		last = got
		// And the diff keeps the larger share — it is what differ is for.
		if m.diffWidth() < got {
			t.Errorf("width %d: diff gets %d columns, file list %d", w, m.diffWidth(), got)
		}
	}
}

// Every row still ends exactly at the panel edge, at whatever width.
func TestResponsive_NothingOverflowsAtAnyWidth(t *testing.T) {
	t.Parallel()
	for _, size := range [][2]int{{60, 10}, {72, 20}, {80, 24}, {100, 30}, {120, 40}, {200, 50}} {
		m := responsiveModel(t, size[0], size[1])
		view := m.View()

		for i, row := range strings.Split(view, "\n") {
			if got := lipgloss.Width(row); got > size[0] {
				t.Errorf("%dx%d: row %d is %d columns: %q",
					size[0], size[1], i, got, stripANSI(row))
			}
		}
		if h := lipgloss.Height(view); h != size[1] {
			t.Errorf("%dx%d: the view is %d rows", size[0], size[1], h)
		}
	}
}

// Below the width two panels need, one panel takes the terminal rather than
// both being squeezed into something unusable.
func TestResponsive_ANarrowTerminalCollapsesToOnePanel(t *testing.T) {
	t.Parallel()
	m := responsiveModel(t, 64, 24)
	if !m.onePanel() {
		t.Fatalf("at 64 columns the layout still splits (threshold %d)", twoPanelWidth)
	}

	// In the file list, that one panel is the file list.
	m.mode = modeFileList
	if got := stripANSI(m.View()); !strings.Contains(got, "CHANGED FILES") {
		t.Errorf("collapsed to the wrong panel in file-list mode:\n%s", got)
	}
	if strings.Contains(stripANSI(m.View()), verticalDivider) {
		t.Error("a collapsed layout still draws the divider")
	}

	// Reading a diff, it is the diff.
	m.mode = modeDiff
	if got := stripANSI(m.View()); strings.Contains(got, "CHANGED FILES") {
		t.Errorf("collapsed to the wrong panel in diff mode:\n%s", got)
	}

	// And the file list gets the whole width when it has it.
	m.mode = modeFileList
	if got := m.listWidth(); got != m.width {
		t.Errorf("the collapsed file list is %d of %d columns", got, m.width)
	}
}

// A terminal too small for even one usable panel still says so rather than
// drawing nonsense — but the wall is far lower than it was.
func TestResponsive_TheWallIsALastResort(t *testing.T) {
	t.Parallel()
	m := responsiveModel(t, 30, 6)
	if got := m.View(); !strings.Contains(got, "too small") {
		t.Errorf("a 30x6 terminal rendered a layout:\n%s", got)
	}
	// 60x10 used to be the wall and now draws something usable.
	m = responsiveModel(t, 60, 10)
	if got := m.View(); strings.Contains(got, "too small") {
		t.Errorf("60x10 still refuses to draw:\n%s", got)
	}
}

// With colour off, every distinction has to survive in the text itself: a
// reviewer over SSH, a 16-colour terminal, NO_COLOR, or a screenshot in
// greyscale.
func TestResponsive_EveryDistinctionSurvivesWithoutColour(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("kept.ts", "one\n", "init")
	tr.CommitFile("gone.ts", "one\n", "second")
	tr.Modify("kept.ts", "one\ntwo\n")
	tr.Stage("kept.ts")
	tr.Delete("gone.ts")
	tr.Untracked("new.ts", "fresh\n")

	m := liveModel(t, tr)
	plain := theme.NoColorTheme()
	m.theme, m.styles = plain, NewStyles(plain)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	list := stripANSI(m.renderFileList())
	// Staged, and each status, by mark rather than by hue.
	for _, want := range []string{"●", "M", "D", "?"} {
		if !strings.Contains(list, want) {
			t.Errorf("the file list does not mark %q without colour:\n%s", want, list)
		}
	}

	// The diff's added and removed lines, and the cursor.
	m.mode = modeDiff
	if cmd := m.loadDiffCmd(true); cmd != nil {
		u, _ := m.Update(cmd())
		m = u.(Model)
	}
	diff := stripANSI(m.renderer.Content(m.diffCursor))
	for _, want := range []string{"+", "-", cursorMarker} {
		if !strings.Contains(diff, want) {
			t.Errorf("the diff does not mark %q without colour:\n%s", want, diff)
		}
	}

	// And the focused panel, which is otherwise only an accent colour.
	if !strings.Contains(stripANSI(m.View()), focusBar) {
		t.Error("focus is not marked without colour")
	}
}

// Review state is words, not colours, so it reads in greyscale too.
func TestResponsive_ReviewStateIsWordsNotColours(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{
		{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "b.ts", Status: git.StatusModified}},
	})
	plain := theme.NoColorTheme()
	m.theme, m.styles = plain, NewStyles(plain)
	m.mode = modeReview
	m.session = review.NewSession()
	m.session.MarkViewed("a.ts")

	got := stripANSI(m.renderFileList())
	if !strings.Contains(got, "read") {
		t.Errorf("review state is not readable without colour:\n%s", got)
	}
}
