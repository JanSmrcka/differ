package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jansmrcka/differ/internal/testutil"
)

// What the issue is about: the agent rewrites the file you are reading and the
// content is swapped underneath you. In review mode that is worse than
// startling — a pending comment can stop matching what is on screen.
//
// The refresh still lands: the file list, its marks and the review counts are
// all current. What is held back is the one thing that would move under the
// reviewer, the diff they are looking at.
func TestNotify_TheDiffIsNotSwappedUnderTheReviewer(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\nthree\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\nthree\n")

	m := reviewing(t, tr)
	before := m.renderer.Content(m.diffCursor)
	if !strings.Contains(before, "CHANGED") {
		t.Fatalf("the diff does not show the change to begin with:\n%s", before)
	}

	// The agent rewrites it while the reviewer is reading.
	tr.Modify("src.ts", "one\nREWRITTEN BY THE AGENT\nthree\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if got := m.renderer.Content(m.diffCursor); got != before {
		t.Errorf("the diff was swapped under the reviewer:\n%s", got)
	}
	if !m.diffStale {
		t.Error("nothing recorded that the diff on screen is out of date")
	}
}

// And the user has to be told, rather than left looking at something stale.
func TestNotify_TheUserIsToldTheDiffMoved(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	if strings.Contains(m.View(), "reload") {
		t.Fatal("the reload notice is showing before anything changed")
	}

	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())

	view := m.View()
	if !strings.Contains(view, "reload") {
		t.Errorf("the user is not told the diff moved:\n%s", view)
	}
}

// reviewing returns a model sitting in review mode with a diff on screen.
func reviewing(t *testing.T, tr *testutil.Repo) Model {
	t.Helper()
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	// One refresh first, the way the session's first tick does it: without a
	// baseline of file keys nothing can be seen to have moved later.
	m = settle(t, m, m.refreshFilesCmd()())
	if m.fileKeys == nil {
		t.Fatal("the first refresh recorded no file keys")
	}
	updated, cmd := m.enterReviewMode()
	m = updated.(Model)
	m = settle(t, m, cmdMsg(cmd))
	if m.mode != modeReview {
		t.Fatalf("mode = %v, want modeReview", m.mode)
	}
	if m.renderer == nil {
		t.Fatal("no diff on screen")
	}
	return m
}

func cmdMsg(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// Reloading is the user's to ask for, and it brings the diff up to date.
func TestNotify_ReloadBringsTheDiffUpToDate(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale {
		t.Fatal("the diff was not held back")
	}

	m = settle(t, m, key(reloadKey))

	if m.diffStale {
		t.Error("the notice is still showing after a reload")
	}
	if got := m.renderer.Content(m.diffCursor); !strings.Contains(got, "REWRITTEN") {
		t.Errorf("the reload did not bring the new content in:\n%s", got)
	}
	if strings.Contains(m.View(), "to reload") {
		t.Error("the reload notice is still on screen")
	}
}

// "Never invalidate the user's current interaction implicitly." A comment
// being written when the repository moves must survive both the refresh and
// the reload — it is the thing the reviewer would most hate to lose.
func TestNotify_AnOpenCommentEditorSurvivesTheChangeAndTheReload(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	updated, _ := m.startComment()
	m = updated.(Model)
	if !m.commenting {
		t.Fatal("the comment editor did not open")
	}
	m.commentInput.SetValue("half a thought about this line")

	// The agent rewrites the file mid-sentence.
	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if !m.commenting {
		t.Fatal("the refresh closed the comment editor")
	}
	if got := m.commentInput.Value(); got != "half a thought about this line" {
		t.Errorf("the refresh cleared the draft: %q", got)
	}

	// And the editor still owns the keyboard: the reload key is a letter while
	// someone is typing, not a command. Reloading waits until they are done.
	m = settle(t, m, key(reloadKey))
	if !m.commenting {
		t.Error("the reload key closed the comment editor")
	}
	if got := m.commentInput.Value(); got != "half a thought about this line"+reloadKey {
		t.Errorf("the reload key did not reach the editor as a character: %q", got)
	}
	if !m.diffStale {
		t.Error("the diff was reloaded under an open comment editor")
	}
}

// The reviewer's place in the diff is theirs. A reload re-reads the file; it
// does not send them back to the top of it.
func TestNotify_ReloadKeepsTheReviewersPlace(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	body := strings.Repeat("keep\n", 40)
	tr.CommitFile("src.ts", body+"two\n", "first")
	tr.Modify("src.ts", body+"CHANGED\n")

	m := reviewing(t, tr)
	for i := 0; i < 12; i++ {
		updated, _ := m.updateDiffMode(key("j"))
		m = updated.(Model)
	}
	cursor := m.diffCursor
	if cursor == 0 {
		t.Fatal("the cursor did not move")
	}

	tr.Modify("src.ts", body+"REWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())
	m = settle(t, m, key(reloadKey))

	if m.diffCursor != cursor {
		t.Errorf("the reload moved the cursor from %d to %d", cursor, m.diffCursor)
	}
}

// Outside review mode differ stays live. Holding the diff everywhere would
// undo the point of the poll — the file list and diff are meant to track the
// repository without being asked.
func TestNotify_OutsideReviewTheDiffStaysLive(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m = settle(t, m, m.refreshFilesCmd()())
	m = settle(t, m, key("enter")) // the plain diff, not review
	if m.mode != modeDiff {
		t.Fatalf("mode = %v, want modeDiff", m.mode)
	}

	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if m.diffStale {
		t.Error("the diff was held back outside review mode")
	}
	if got := m.renderer.Content(m.diffCursor); !strings.Contains(got, "REWRITTEN") {
		t.Errorf("the plain diff did not follow the repository:\n%s", got)
	}
}

// "Stale comments are visually distinguishable after a reload." A comment
// whose line is gone cannot be silently dropped or left pointing at whatever
// now occupies that line number — it has to say what happened.
func TestNotify_ACommentWhoseLineVanishedIsMarkedStaleAfterReload(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\nthree\n", "first")
	tr.Modify("src.ts", "one\nDOOMED\nthree\n")

	m := reviewing(t, tr)
	m = m.cursorTo(t, "DOOMED")
	updated, _ := m.startComment()
	m = updated.(Model)
	m.commentInput.SetValue("this line worries me")
	updated, _ = m.saveComment()
	m = updated.(Model)

	if n := m.session.CountFor("src.ts"); n != 1 {
		t.Fatalf("the comment was not saved (%d on the file)", n)
	}
	if m.session.StaleCount() != 0 {
		t.Fatal("the comment is stale before anything changed")
	}

	// The agent removes the very line it was about.
	tr.Modify("src.ts", "one\nthree\n")
	m = settle(t, m, m.refreshFilesCmd()())
	m = settle(t, m, key(reloadKey))

	if m.session.CountFor("src.ts") != 1 {
		t.Error("the comment was dropped rather than marked")
	}
	if m.session.StaleCount() != 1 {
		t.Errorf("the comment is not stale: %d stale of %d",
			m.session.StaleCount(), m.session.CountFor("src.ts"))
	}
	if !strings.Contains(m.View(), "stale") {
		t.Errorf("nothing on screen says the comment is stale:\n%s", m.View())
	}
}

// cursorTo moves the diff cursor onto the line containing text.
func (m Model) cursorTo(t *testing.T, text string) Model {
	t.Helper()
	for i, l := range m.renderer.Parsed().Lines {
		if strings.Contains(l.Content, text) {
			return m.setCursor(i)
		}
	}
	t.Fatalf("no diff line contains %q", text)
	return m
}
