package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/testutil"
)

func chromeModel(t *testing.T, w, h int) Model {
	t.Helper()
	tr := testutil.NewRepo(t)
	tr.AgentChangeset()
	m := liveModel(t, tr)
	u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return u.(Model)
}

// The panels become a divider and rules rather than two bordered cards.
func TestChrome_NoBoxDrawnCards(t *testing.T) {
	view := chromeModel(t, 120, 30).View()
	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if strings.Contains(view, corner) {
			t.Errorf("view still draws card corners (%s):\n%s", corner, view)
		}
	}
}

func TestChrome_HeaderNamesTheToolAndBranch(t *testing.T) {
	m := chromeModel(t, 120, 30)
	header := strings.Split(m.View(), "\n")[0]

	if !strings.Contains(header, "differ") {
		t.Errorf("header should name the tool: %q", header)
	}
	if !strings.Contains(header, "master") {
		t.Errorf("header should show the branch: %q", header)
	}
}

// The changeset is summarised where it is listed: the file list's own header
// row. It used to be in the header bar as well, and a screen that states the
// same count twice reads as chrome rather than as information.
func TestChrome_TheFileListHeaderSummarisesTheChangeset(t *testing.T) {
	m := chromeModel(t, 120, 30)
	label := strings.Split(stripANSI(m.View()), "\n")[2]
	left, _, ok := strings.Cut(label, verticalDivider)
	if !ok {
		t.Fatalf("no divider in the label row: %q", label)
	}

	if !strings.Contains(left, "4") {
		t.Errorf("the file list should count the files: %q", left)
	}
	if !strings.Contains(left, "1 staged") {
		t.Errorf("the file list should report staged state: %q", left)
	}
}

func TestChrome_HasRulesAndDivider(t *testing.T) {
	m := chromeModel(t, 120, 30)
	lines := strings.Split(m.View(), "\n")

	rules := 0
	for _, l := range lines {
		if strings.Count(l, "─") > 20 {
			rules++
		}
	}
	if rules < 2 {
		t.Errorf("expected a rule above and below the content, found %d", rules)
	}

	divided := 0
	for _, l := range lines {
		if strings.Contains(l, verticalDivider) {
			divided++
		}
	}
	if divided < 5 {
		t.Errorf("expected the panels to be separated by a divider on most rows, got %d", divided)
	}
}

// Focus has to be visible without colour, since the test terminal has none.
func TestChrome_FocusIsVisibleWithoutColour(t *testing.T) {
	m := chromeModel(t, 120, 30)
	m.mode = modeFileList
	files := m.View()
	m.mode = modeDiff
	diff := m.View()

	if files == diff {
		t.Error("the focused panel is indistinguishable between file list and diff mode")
	}
}

func TestChrome_FitsTerminal(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {100, 30}, {120, 40}} {
		m := chromeModel(t, size.w, size.h)
		if got := len(strings.Split(m.View(), "\n")); got > size.h {
			t.Errorf("at %dx%d the view is %d lines", size.w, size.h, got)
		}
		for i, l := range strings.Split(m.View(), "\n") {
			if w := lipglossWidth(l); w > size.w {
				t.Errorf("at %dx%d line %d is %d columns: %q", size.w, size.h, i, w, l)
				break
			}
		}
	}
}

// The diff should get the room, not the chrome.
func TestChrome_DiffGetsMostOfTheWidth(t *testing.T) {
	m := chromeModel(t, 120, 30)
	if got := m.diffWidth(); got < 70 {
		t.Errorf("diff panel is %d columns of 120 — chrome is taking too much", got)
	}
}
