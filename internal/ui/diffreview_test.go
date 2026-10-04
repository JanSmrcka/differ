package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
)

// Review is not a mode you enter: the diff is where you read, so it is where
// you comment. A separate mode put a keypress between reading a line and
// saying something about it, and made `r` mean three things.

func TestDiffReview_CommentStraightFromTheDiff(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	m.mode = modeFileList

	m = pressIn(t, m, "enter")
	m = pressIn(t, m, "c")

	if m.mode != modeDiff {
		t.Fatalf("mode = %v, want modeDiff", m.mode)
	}
	if !m.commenting {
		t.Error("c in the diff did not open the comment editor")
	}
}

func TestDiffReview_OpeningADiffMarksItViewed(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	m.mode = modeFileList

	m = pressIn(t, m, "enter")

	if m.session == nil {
		t.Fatal("no session to record the visit in")
	}
	if got := m.session.FileStateOf(m.currentFilePath()); got != review.FileViewed {
		t.Errorf("state = %v, want viewed", got)
	}
}

func TestDiffReview_ASessionExistsFromTheStart(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")

	if m := liveModel(t, tr); m.session == nil {
		t.Error("the model starts without a review session")
	}
}

// A diff with nothing written against it follows the repository: differ is
// left running beside an agent, and a diff frozen at the moment it was opened
// would undo the poll.
func TestDiffReview_WithoutCommentsTheDiffStaysLive(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m = settle(t, m, m.refreshFilesCmd()())
	m = settle(t, m, key("enter"))

	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if got := m.renderer.Content(m.diffCursor); !strings.Contains(got, "REWRITTEN") {
		t.Errorf("a diff with no comments did not follow the repository:\n%s", got)
	}
}

// Once a comment is written against the file, a silent swap could leave it
// describing something no longer on screen — so the diff is held and the
// reviewer is asked.
func TestDiffReview_WithACommentTheDiffIsHeld(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m = settle(t, m, m.refreshFilesCmd()())
	m = settle(t, m, key("enter"))
	m.session.Add(review.Comment{File: "src.ts", StartLine: 1, EndLine: 1, Body: "hold"})

	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if got := m.renderer.Content(m.diffCursor); strings.Contains(got, "REWRITTEN") {
		t.Errorf("the diff was swapped under a comment:\n%s", got)
	}
	if !strings.Contains(m.View(), "to reload") {
		t.Error("the reviewer was not told the diff moved")
	}
}
