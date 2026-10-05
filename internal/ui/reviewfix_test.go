package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/testutil"
)

// PR #109 review: the commit input's width was set on a copy inside View, so
// the model's stayed 0 — no scrolling — and a long message ran past the box,
// where the row was clipped with the cursor and the newest text in it.
func TestReviewFix_ALongCommitMessageKeepsItsEndInTheBox(t *testing.T) {
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.go", Status: git.StatusModified, Staged: true}}})
	m.ready = true
	m = m.fitInputsToPanels()
	m.mode = modeCommit
	m.commitInput.Focus()
	m.commitInput.SetValue(strings.Repeat("word ", 40) + "THEEND")
	m.commitInput.CursorEnd()

	if m.commitInput.Width <= 0 || m.commitInput.Width >= m.modalInnerWidth() {
		t.Fatalf("commit input width %d, want within the box (%d)", m.commitInput.Width, m.modalInnerWidth())
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "THEEND") {
		t.Errorf("the end of the message, where the cursor is, is not on screen:\n%s", view)
	}
}

// A comment that has been sent is the agent's to act on now. Holding the diff
// for it froze the view exactly when the fix was arriving.
func TestReviewFix_ASentCommentDoesNotHoldTheDiff(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	var ids []string
	for _, c := range m.session.CommentsFor("src.ts") {
		ids = append(ids, c.ID)
	}
	m.session.MarkSent(ids)

	tr.Modify("src.ts", "one\nTHE FIX\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if got := m.renderer.Content(m.diffCursor); !strings.Contains(got, "THE FIX") {
		t.Errorf("the diff is held for a comment already sent:\n%s", got)
	}
}

// Sending the last pending comment releases a diff that was held and has
// since moved: it reloads rather than sitting stale with no notice.
func TestReviewFix_SendingReleasesAHeldDiff(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Modify("src.ts", "one\nMOVED\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale() {
		t.Fatal("the diff was not held to begin with")
	}

	var ids []string
	for _, c := range m.session.CommentsFor("src.ts") {
		ids = append(ids, c.ID)
	}
	u, cmd := m.handleFeedbackSent(feedbackSentMsg{ids: ids, target: "clipboard"})
	m = settle(t, u.(Model), cmdMsg(cmd))

	if got := m.renderer.Content(m.diffCursor); !strings.Contains(got, "MOVED") {
		t.Errorf("the released diff did not catch up:\n%s", got)
	}
}

// And deleting the last comment does the same.
func TestReviewFix_DeletingReleasesAHeldDiff(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Modify("src.ts", "one\nMOVED\n")
	m = settle(t, m, m.refreshFilesCmd()())

	m = m.cursorTo(t, "one")
	u, cmd := m.deleteCommentAtCursor()
	m = settle(t, u.(Model), cmdMsg(cmd))

	if m.session.CountFor("src.ts") != 0 {
		t.Fatal("the comment was not deleted")
	}
	if got := m.renderer.Content(m.diffCursor); !strings.Contains(got, "MOVED") {
		t.Errorf("the released diff did not catch up:\n%s", got)
	}
}

// A refresh records the new keys before its own reload lands, so diffStale
// is briefly true on a diff nobody is holding. The notice must not flash.
func TestReviewFix_NoNoticeOnADiffThatIsNotHeld(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m = settle(t, m, m.refreshFilesCmd()())
	m = settle(t, m, key("enter"))

	tr.Modify("src.ts", "one\nREWRITTEN\n")
	u, _ := m.Update(m.refreshFilesCmd()()) // keys installed, reload not yet landed
	m = u.(Model)

	if strings.Contains(m.View(), "to reload") {
		t.Error("the notice flashed on a diff that is not held")
	}
}
