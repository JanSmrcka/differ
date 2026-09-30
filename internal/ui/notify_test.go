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

// The hold has to work for the edits agents actually make.
//
// It lived inside the filesEqual branch, and filesEqual compares AddedLines
// and DeletedLines — so any edit that changed how many lines the diff adds or
// removes fell straight through to a reload that reset the cursor, while the
// notice still claimed the diff was being held. Every fixture in this file was
// a one-line replacement, which is the only shape that worked.
func TestNotify_TheDiffIsHeldForEveryShapeOfEdit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, then string }{
		{"same line count", "one\nREWRITTEN\nthree\n"},
		{"a line added", "one\nCHANGED\nEXTRA\nthree\n"},
		{"a line deleted", "one\nthree\n"},
		{"a second line edited too", "one\nCHANGED\nALSO\n"},
		{"the whole file rewritten", "completely\ndifferent\ncontent\nhere\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tr := testutil.NewRepo(t)
			tr.CommitFile("src.ts", "one\ntwo\nthree\n", "first")
			tr.Modify("src.ts", "one\nCHANGED\nthree\n")

			m := reviewing(t, tr)
			before := m.renderer.Content(m.diffCursor)
			cursor := m.diffCursor

			tr.Modify("src.ts", tc.then)
			m = settle(t, m, m.refreshFilesCmd()())

			if got := m.renderer.Content(m.diffCursor); got != before {
				t.Errorf("the diff was swapped under the reviewer:\n%s", got)
			}
			if m.diffCursor != cursor {
				t.Errorf("the cursor moved from %d to %d", cursor, m.diffCursor)
			}
			if !m.diffStale {
				t.Error("no notice that the diff is out of date")
			}
		})
	}
}

// The hold belongs to review mode. It was not gated on it, so leaving review
// with the notice up left the flag set — and the plain diff view, which the
// poll exists to keep live, then refused every refresh for the rest of the
// session.
func TestNotify_LeavingReviewDoesNotFreezeThePlainDiff(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale {
		t.Fatal("the diff was not held")
	}

	// r leaves review mode, notice still up.
	m = settle(t, m, key("r"))
	if m.mode != modeDiff {
		t.Fatalf("mode = %v, want modeDiff", m.mode)
	}

	// The plain diff must go live again.
	tr.Modify("src.ts", "one\nAGAIN\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if got := m.renderer.Content(m.diffCursor); !strings.Contains(got, "AGAIN") {
		t.Errorf("the plain diff is frozen:\n%s", got)
	}
	if strings.Contains(m.View(), "to reload") {
		t.Error("the notice is still up outside review mode")
	}
}

// The notice describes the diff on screen. Move to another file and it is
// describing something the user is no longer looking at.
func TestNotify_MovingToAnotherFileClearsTheNotice(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "first")
	tr.CommitFile("b.ts", "one\ntwo\n", "second")
	tr.Modify("a.ts", "one\nCHANGED\n")
	tr.Modify("b.ts", "one\nOTHER\n")

	m := reviewing(t, tr)
	tr.Modify("a.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale {
		t.Fatal("the diff was not held")
	}

	m = settle(t, m, key("n")) // next file

	if m.diffStale {
		t.Error("the notice followed the cursor to a file it does not describe")
	}
	if strings.Contains(m.View(), "to reload") {
		t.Errorf("the notice is still on screen after moving file:\n%s", m.View())
	}
}

// Holding the display must not hold the bookkeeping. #44 refuses to send a
// stale comment without a second press, and staleness is assigned by
// Reanchor — which ran only when a diff was loaded. Under the hold it never
// ran, so a comment about a line the agent had already deleted was still
// "pending" and went out on the first press, carrying an excerpt of code that
// no longer exists.
func TestNotify_ACommentGoesStaleEvenWhileTheDiffIsHeld(t *testing.T) {
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
	if m.session.StaleCount() != 0 {
		t.Fatal("the comment is stale before anything changed")
	}

	// The agent removes the line the comment is about. No reload yet.
	tr.Modify("src.ts", "one\nthree\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale {
		t.Fatal("the diff was not held")
	}

	if m.session.StaleCount() != 1 {
		t.Errorf("the comment is still pending while the line it describes is gone: %d stale",
			m.session.StaleCount())
	}
}

// "An explicit reload action, with a summary of what changed." Telling the
// user only that something moved is half of that.
func TestNotify_TheNoticeSaysWhatChanged(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\nthree\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\nthree\n")

	m := reviewing(t, tr)
	// Two more lines added on top of the existing edit.
	tr.Modify("src.ts", "one\nCHANGED\nEXTRA\nMORE\nthree\n")
	m = settle(t, m, m.refreshFilesCmd()())

	view := m.View()
	if !strings.Contains(view, "reload") {
		t.Fatalf("no notice at all:\n%s", view)
	}
	// The diff went from +1/-1 to +3/-1: two lines more than when it was read.
	// Asserting on "2" alone would be satisfied by any line number on screen.
	if !strings.Contains(view, "+2 added") {
		t.Errorf("the notice does not say what changed:\n%s", view)
	}
}

// The quiet case, which nothing asserted: a poll that finds nothing must not
// hold the diff or raise a notice. currentFileMoved could have returned true
// unconditionally — freezing the diff and showing "diff moved" on every tick
// for a repository nobody touched — and the whole suite stayed green.
func TestNotify_AnUntouchedRepositoryRaisesNoNotice(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "first")
	tr.CommitFile("b.ts", "one\ntwo\n", "second")
	tr.Modify("a.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	for i := 0; i < 5; i++ {
		m = settle(t, m, m.refreshFilesCmd()())
		if m.diffStale {
			t.Fatalf("poll %d raised a notice with nothing changed", i+1)
		}
	}
	if strings.Contains(m.View(), "to reload") {
		t.Errorf("a notice appeared with nothing changed:\n%s", m.View())
	}

	// And a change to a *different* file is still not this diff moving.
	tr.Modify("b.ts", "one\nOTHER\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if m.diffStale {
		t.Error("another file changing raised a notice about this one")
	}
}

// A file appearing or vanishing is a change to the list, not to the diff on
// screen — the list refreshes either way. Treating an unknown side as movement
// would raise a notice for every file the agent adds.
func TestNotify_AFileAppearingElsewhereRaisesNoNotice(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "first")
	tr.Modify("a.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Untracked("brand-new.ts", "fresh\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if m.diffStale {
		t.Error("a new file elsewhere raised a notice about the diff on screen")
	}
}

// Entering review before the first diff has landed must not raise a notice
// about a diff that is not on screen yet.
func TestNotify_NoNoticeBeforeADiffIsOnScreen(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "first")
	tr.Modify("a.ts", "one\nCHANGED\n")

	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m = settle(t, m, m.refreshFilesCmd()())
	m.mode = modeReview
	m.renderer = nil // nothing drawn yet

	tr.Modify("a.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if m.diffStale {
		t.Error("a notice was raised before any diff was on screen")
	}
}

// R does nothing when there is nothing to reload, rather than throwing away
// the reviewer's position for no reason.
func TestNotify_ReloadWithNothingStaleIsANoOp(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "first")
	tr.Modify("a.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	_, cmd := m.reloadDiff()
	if cmd != nil {
		t.Error("R reloaded a diff that was already current")
	}
}

// The editor jumps to a line number read out of the diff on screen. While that
// diff is held it describes a file that has since moved, so the number is
// wrong — and opening someone's editor at a confidently wrong line is worse
// than opening it at the top.
func TestNotify_TheEditorDoesNotJumpToALineFromAHeldDiff(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", strings.Repeat("keep\n", 10)+"two\n", "first")
	tr.Modify("src.ts", strings.Repeat("keep\n", 10)+"CHANGED\n")

	m := reviewing(t, tr)
	m = m.cursorTo(t, "CHANGED")
	if m.editorLine() == 0 {
		t.Fatal("no line under the cursor to begin with")
	}

	// The agent moves everything, so the number no longer means anything.
	tr.Modify("src.ts", "PREPENDED\n"+strings.Repeat("keep\n", 10)+"CHANGED\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale {
		t.Fatal("the diff was not held")
	}

	if got := m.editorLine(); got != 0 {
		t.Errorf("the editor would jump to line %d, read from a diff that is out of date", got)
	}
}

// Between pressing n and the new diff arriving, the renderer is still the
// previous file's. A refresh landing in that window must not raise a notice:
// it would be comparing the cursor's file against a diff of a different one,
// and the load already on its way brings current content regardless.
func TestNotify_NoNoticeWhileTheCursorAndTheDiffDisagree(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "first")
	tr.CommitFile("b.ts", "one\ntwo\n", "second")
	tr.Modify("a.ts", "one\nCHANGED\n")
	tr.Modify("b.ts", "one\nOTHER\n")

	m := reviewing(t, tr)
	if m.rendererPath != m.currentFilePath() {
		t.Fatal("the renderer and the cursor disagree to begin with")
	}

	// Move the cursor without letting the new diff land.
	updated, _ := m.updateReviewMode(key("n"))
	m = updated.(Model)
	if m.rendererPath == m.currentFilePath() {
		t.Skip("the diff loaded synchronously here, so there is no window to test")
	}

	tr.Modify("a.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if m.diffStale {
		t.Error("a notice was raised while the cursor and the diff on screen disagreed")
	}
}
