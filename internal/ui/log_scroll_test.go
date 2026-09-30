package ui

import (
	"strings"
	"testing"

	"github.com/jansmrcka/differ/internal/git"
)

// The cursor has to be on screen.
//
// viewList drew commits[:contentHeight] while the cursor could reach the last
// of up to a hundred commits — so in a short terminal pressing G selected a
// row nobody could see, and enter opened a commit whose hash appeared nowhere.
// The changed-file list got a scroll offset in #52; this list is the same
// problem and #48 brought it into the same frame.
func TestLog_TheCursorStaysOnScreen(t *testing.T) {
	t.Parallel()
	commits := make([]git.Commit, 60)
	for i := range commits {
		commits[i] = git.Commit{
			Short:   "abc" + strings.Repeat("0", 4) + string(rune('a'+i%26)),
			Subject: "commit number " + string(rune('a'+i%26)),
			Date:    "2026-09-30",
		}
	}

	for _, height := range []int{10, 14, 20, 40} {
		m := newLogModelForTest(commits, 100, height)
		for _, cursor := range []int{0, 1, 7, 30, 58, 59} {
			m.cursor = cursor
			m = m.clampLogScroll()
			view := m.View()
			want := commits[cursor].Subject

			var selected string
			for _, row := range strings.Split(view, "\n") {
				if strings.Contains(row, cursorMarker) || strings.HasPrefix(strings.TrimSpace(row), focusBar) {
					selected = row
				}
			}
			if !strings.Contains(view, want) {
				t.Errorf("h=%d cursor=%d: the selected commit %q is not on screen",
					height, cursor, want)
				continue
			}
			_ = selected
		}
	}
}

// newLogModelForTest builds a LogModel with a list and a size, without a repo.
func newLogModelForTest(commits []git.Commit, width, height int) LogModel {
	_, th := testStyles()
	m := NewLogModel(nil, NewStyles(th), th, 4)
	m.commits = commits
	m.width = width
	m.height = height
	m.ready = true
	return m
}
