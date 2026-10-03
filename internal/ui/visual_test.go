package ui

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/testutil"
	"github.com/jansmrcka/differ/internal/theme"
)

// One visual language across the application.
//
// The main view lost its bordered cards, but the commit log kept them, so
// `differ` and `differ log` looked like two different programs. These tests
// pin the frame down rather than leaving it to whoever edits next.

// stripANSI removes colour so a test can ask whether two rows differ in
// anything but their colour.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiEscape.ReplaceAllString(s, "") }

func logModelOn(t *testing.T, width, height int) LogModel {
	t.Helper()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "first")
	tr.CommitFile("b.txt", "two\n", "second")

	repo, err := git.NewRepo(tr.Dir)
	if err != nil {
		t.Fatal(err)
	}
	th := theme.Themes["dark"]
	m := NewLogModel(repo, NewStyles(th), th, 4)
	u, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	lm := u.(LogModel)
	if cmd := lm.Init(); cmd != nil {
		u, _ = lm.Update(cmd())
		lm = u.(LogModel)
	}
	return lm
}

// The box-drawing corners are the giveaway: the main view has none, so the
// log must not either.
func TestVisual_NoScreenDrawsBoxes(t *testing.T) {
	corners := []string{"╭", "╮", "╰", "╯"}

	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	main := liveModel(t, tr)
	for _, mode := range []viewMode{modeFileList, modeDiff, modeReview} {
		main.mode = mode
		for _, c := range corners {
			if strings.Contains(main.View(), c) {
				t.Errorf("mode %v still draws a box (%s)", mode, c)
			}
		}
	}

	lm := logModelOn(t, 120, 32)
	for _, c := range corners {
		if strings.Contains(lm.View(), c) {
			t.Errorf("the log browser still draws a box (%s):\n%s", c, lm.View())
		}
	}
}

// Both programs are framed the same way: a header, a rule, the content, a
// rule, then one bar.
func TestVisual_TheLogBrowserSharesTheFrame(t *testing.T) {
	lm := logModelOn(t, 120, 32)
	view := lm.View()

	if !strings.Contains(view, strings.Repeat(horizontalRule, 10)) {
		t.Errorf("the log browser has no rules:\n%s", view)
	}
	if !strings.Contains(view, "differ") {
		t.Errorf("the log browser has no header:\n%s", view)
	}
	if h := lipgloss.Height(view); h != 32 {
		t.Errorf("the log view is %d rows, want the full 32", h)
	}
}

// A view wider than the terminal is what the old card arithmetic kept getting
// wrong, and it breaks the whole layout when it happens.
func TestVisual_NoScreenIsWiderThanTheTerminal(t *testing.T) {
	for _, width := range []int{80, 100, 120} {
		lm := logModelOn(t, width, 30)
		for _, line := range strings.Split(lm.View(), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("log at width %d has a %d-column row: %q", width, w, line)
			}
		}
	}
}

// The selected row has to be unmistakable in every list, and recognisable
// without colour — a reviewer over SSH or with NO_COLOR set still needs to
// see where the cursor is.
func TestVisual_TheSelectionIsVisibleWithoutColour(t *testing.T) {
	lm := logModelOn(t, 120, 32)
	plain := NewStyles(theme.NoColorTheme())
	lm.styles = plain

	// The *same* commit, rendered both ways. It used to compare commits[0]
	// selected against commits[1] unselected — two different commits, so the
	// plain text differed whatever the styling did, and the assertion could
	// not fail.
	selected := stripANSI(lm.renderCommitLine(lm.commits[0], true))
	plainRow := stripANSI(lm.renderCommitLine(lm.commits[0], false))
	if selected == plainRow {
		t.Errorf("selected and unselected rows are identical without colour:\n%q\n%q",
			selected, plainRow)
	}
	if !strings.Contains(selected, cursorMarker) {
		t.Errorf("the selected row carries no marker: %q", selected)
	}
	if strings.Contains(plainRow, cursorMarker) {
		t.Errorf("an unselected row carries the marker: %q", plainRow)
	}
	// And the two must be the same width, or the list jitters as the cursor
	// moves.
	if lipgloss.Width(selected) != lipgloss.Width(plainRow) {
		t.Errorf("selected row is %d wide, unselected %d",
			lipgloss.Width(selected), lipgloss.Width(plainRow))
	}
}

// The command bar is the one place keys are listed, in both programs.
func TestVisual_TheLogBrowserUsesTheCommandBar(t *testing.T) {
	lm := logModelOn(t, 120, 32)
	view := lm.View()
	for _, want := range []string{"q", "quit"} {
		if !strings.Contains(view, want) {
			t.Errorf("the log browser does not offer %q:\n%s", want, view)
		}
	}
}

// Overlong labels were once reported as a broken layout: a long file name
// made a panel title wider than the panel and ragged the whole left column.
// The card that bug lived in is gone, but truncateEnd is what keeps every
// label inside its space, and it had no test of its own.
func TestTruncateEnd(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		in    string
		maxW  int
		want  string
		width int
	}{
		{name: "short enough is untouched", in: "src.ts", maxW: 10, want: "src.ts", width: 6},
		{name: "exactly the limit is untouched", in: "src.ts", maxW: 6, want: "src.ts", width: 6},
		{name: "too long is cut with an ellipsis", in: "docs/validate-user-guide", maxW: 10, width: 10},
		{name: "one column is just the ellipsis", in: "src.ts", maxW: 1, want: "…", width: 1},
		{name: "no room at all is empty", in: "src.ts", maxW: 0, want: "", width: 0},
		{name: "a negative limit is empty", in: "src.ts", maxW: -5, want: "", width: 0},
		// A multibyte label must not be cut mid-character.
		{name: "wide characters are counted as columns", in: "žluťoučký kůň", maxW: 8, width: 8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := truncateEnd(c.in, c.maxW)
			if c.want != "" && got != c.want {
				t.Errorf("truncateEnd(%q, %d) = %q, want %q", c.in, c.maxW, got, c.want)
			}
			if w := lipgloss.Width(got); w > c.maxW && c.maxW > 0 {
				t.Errorf("truncateEnd(%q, %d) = %q, %d columns wide", c.in, c.maxW, got, w)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncateEnd(%q, %d) produced invalid UTF-8: %q", c.in, c.maxW, got)
			}
		})
	}
}

// `differ review` in a clean repository has to say so.
//
// This reverses what the test used to assert. It required review mode *not* to
// open, with "nothing to review" in the status bar — but the panel then said
// "No changes / Your working tree is clean.", so the two disagreed about the
// same question, and the wording the issue asked for appeared nowhere. Review
// mode opens and the panel answers.
func TestStartInReviewMode_SaysSoWhenThereIsNothingToReview(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.txt", "one\n", "only commit")
	m := liveModel(t, tr)
	if len(m.files) != 0 {
		t.Skipf("expected a clean repo, got %d files", len(m.files))
	}

	m.StartInReviewMode()
	if m.mode != modeReview {
		t.Error("differ review did not open review mode")
	}
	got := stripANSI(m.renderFileList())
	if !strings.Contains(got, "Nothing to review") {
		t.Errorf("the panel does not say there is nothing to review:\n%s", got)
	}
}
