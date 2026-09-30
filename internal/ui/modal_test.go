package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/testutil"
)

func modalModel(t *testing.T, w, h int) Model {
	t.Helper()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\nthree\nfour\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\nthree\nfour\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: w, Height: h})
	return settle(t, m, key("r"))
}

// A modal is a box over the view, not a replacement for it: the frame stays
// exactly the terminal, nothing overflows, and the view shows around the box.
func TestModal_IsABoxOverTheViewAtEverySize(t *testing.T) {
	t.Parallel()
	for _, w := range []int{40, 60, 80, 120, 200} {
		for _, h := range []int{10, 14, 24, 40} {
			m := modalModel(t, w, h)
			updated, _ := m.updateReviewMode(key("c"))
			m = updated.(Model)
			view := m.View()

			rows := strings.Split(view, "\n")
			if len(rows) != h {
				t.Errorf("%dx%d: frame is %d rows", w, h, len(rows))
			}
			for i, row := range rows {
				if got := lipgloss.Width(row); got > w {
					t.Errorf("%dx%d: row %d is %d wide", w, h, i, got)
					break
				}
			}
		}
	}
}

// Covering a row must not mark it as truncated. truncateEnd appends the
// truncation glyph, which put a "…" against the left edge of every modal row.
func TestModal_CoveringARowDoesNotMarkItTruncated(t *testing.T) {
	t.Parallel()
	m := modalModel(t, 96, 22)
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)

	for i, row := range strings.Split(m.View(), "\n") {
		if !strings.Contains(row, "│") && !strings.Contains(row, "╭") {
			continue // not a row the modal covers
		}
		if strings.Contains(row, truncationMarker) {
			t.Errorf("row %d is marked truncated where the modal covers it:\n%s", i, row)
		}
	}
}

// The view is still there around the box — that is the point of a modal over
// a footer bar.
func TestModal_TheViewShowsAroundIt(t *testing.T) {
	t.Parallel()
	m := modalModel(t, 120, 24)
	before := m.View()
	if !strings.Contains(before, "CHANGED FILES") {
		t.Fatal("the file list is not on screen to begin with")
	}

	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)
	view := m.View()

	if !strings.Contains(view, "CHANGED FILES") {
		t.Errorf("the modal blanked the view behind it:\n%s", view)
	}
	if !strings.Contains(view, "comment · line") {
		t.Errorf("the modal is not drawn:\n%s", view)
	}
}

// Only one question at a time.
func TestModal_OnlyOneIsEverDrawn(t *testing.T) {
	t.Parallel()
	m := modalModel(t, 120, 24)
	updated, _ := m.updateReviewMode(key("c"))
	m = updated.(Model)
	m.showAgents = true // both flags set, which should not be reachable

	view := m.View()
	corners := strings.Count(view, "╭")
	if corners != 1 {
		t.Errorf("%d modals drawn at once:\n%s", corners, view)
	}
}
