package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
)

func pressIn(t *testing.T, m Model, k string) Model {
	t.Helper()
	updated, _ := m.Update(key(k))
	return updated.(Model)
}

// openIn opens the file under the cursor the way the reader does: from the
// file list, with enter.
func openIn(t *testing.T, m Model) Model {
	t.Helper()
	m.mode = modeFileList
	return pressIn(t, m, "enter")
}

func TestReviewMode_EnteredFromFileList(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	m.mode = modeFileList

	m = openIn(t, m)

	if m.mode != modeDiff {
		t.Fatalf("mode = %v, want modeDiff", m.mode)
	}
	if m.session == nil {
		t.Fatal("entering review mode must create a session")
	}
}

func TestReviewMode_EnteredFromDiffViewKeepsCursor(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	m = press(t, m, "j", "j")
	before := m.diffCursor

	m = openIn(t, m)

	if m.mode != modeDiff {
		t.Fatalf("mode = %v, want modeDiff", m.mode)
	}
	if m.diffCursor != before {
		t.Errorf("entering review moved the cursor from %d to %d", before, m.diffCursor)
	}
}

func TestReviewMode_EscReturnsToFileList(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	m = openIn(t, m)

	updated, _ := m.updateDiffMode(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.mode != modeFileList {
		t.Errorf("mode = %v, want modeFileList", m.mode)
	}
}

func TestReviewMode_SessionSurvivesLeavingAndReentering(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	m = openIn(t, m)
	m.session.Add(review.Comment{File: "src.ts", StartLine: 2, EndLine: 2, Body: "keep me"})

	updated, _ := m.updateDiffMode(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	m = openIn(t, m)

	if got := m.session.CountFor("src.ts"); got != 1 {
		t.Errorf("comment count after re-entering = %d, want 1", got)
	}
}

func TestReviewMode_NavigationWorks(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	m = openIn(t, m)
	start := m.diffCursor

	updated, _ := m.updateDiffMode(key("j"))
	m = updated.(Model)
	if m.diffCursor != start+1 {
		t.Errorf("j in review mode: cursor = %d, want %d", m.diffCursor, start+1)
	}

	updated, _ = m.updateDiffMode(key("}"))
	m = updated.(Model)
	if want := m.renderer.Parsed().Hunks[1].StartLine + 1; m.diffCursor != want {
		t.Errorf("} in review mode: cursor = %d, want %d", m.diffCursor, want)
	}
}

func TestReviewMode_MarksCurrentFileViewed(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	m = openIn(t, m)

	if got := m.session.FileStateOf("src.ts"); got != review.FileViewed {
		t.Errorf("file state = %v, want viewed", got)
	}
}

func TestReviewMode_StatusBarShowsProgress(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	m = openIn(t, m)

	m.width = 100
	if seg := m.statusSegment(); !strings.Contains(seg, "1/1") {
		t.Errorf("footer should show review progress: %q", seg)
	}
}

func TestReviewMode_HelpBarIsReviewSpecific(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	m = openIn(t, m)

	help := m.renderHintBar()
	if !strings.Contains(help, "hunk") {
		t.Errorf("review help should mention hunk navigation: %q", help)
	}
	if strings.Contains(help, "commit") {
		t.Errorf("review help should not offer commit: %q", help)
	}
}

// The load-bearing guarantee: review mode is a viewer, not a git client.
func TestReviewMode_DoesNotTouchGitState(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	before := strings.Join(tr.Status(), "\n")
	beforeHead := tr.Git("rev-parse", "HEAD")

	m := liveModel(t, tr)
	m = openIn(t, m)
	for _, k := range []string{"j", "j", "}", "k", "{", "G", "g"} {
		updated, _ := m.updateDiffMode(key(k))
		m = updated.(Model)
	}

	if after := strings.Join(tr.Status(), "\n"); after != before {
		t.Errorf("review mode changed the working tree:\n before:\n%s\n after:\n%s", before, after)
	}
	if after := tr.Git("rev-parse", "HEAD"); after != beforeHead {
		t.Errorf("review mode moved HEAD from %s to %s", beforeHead, after)
	}
}

func TestReviewMode_StagingStillReachableFromFileList(t *testing.T) {
	// Review mode must not break the existing git flows.
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.go", Status: git.StatusModified}}})
	m.mode = modeFileList
	help := m.renderHintBar()
	for _, want := range []string{"stage", "commit"} {
		if !strings.Contains(help, want) {
			t.Errorf("file list help lost %q: %q", want, help)
		}
	}
}

func TestReviewMode_MovingToNextFileMarksItViewed(t *testing.T) {
	m := newTestModel(t, []fileItem{
		{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "b.ts", Status: git.StatusModified}},
	})
	m.mode = modeFileList
	m = openIn(t, m)

	if got := m.session.FileStateOf("b.ts"); got != review.FileUnreviewed {
		t.Fatalf("b.ts starts %v, want unreviewed", got)
	}

	updated, _ := m.updateDiffMode(key("n"))
	m = updated.(Model)

	if got := m.session.FileStateOf("b.ts"); got != review.FileViewed {
		t.Errorf("after moving to b.ts state = %v, want viewed", got)
	}
	if p := m.reviewProgress(); p.Reviewed != 2 {
		t.Errorf("progress Reviewed = %d, want 2", p.Reviewed)
	}
}

func TestReviewMode_ProgressCountsOnlyVisitedFiles(t *testing.T) {
	m := newTestModel(t, []fileItem{
		{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "b.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "c.ts", Status: git.StatusModified}},
	})
	m.mode = modeFileList
	m = openIn(t, m)

	p := m.reviewProgress()
	if p.Reviewed != 1 || p.Total != 3 {
		t.Errorf("progress = %d/%d, want 1/3", p.Reviewed, p.Total)
	}
}
