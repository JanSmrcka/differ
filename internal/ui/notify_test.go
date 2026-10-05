package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jansmrcka/differ/internal/review"
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
	if !m.diffStale() {
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
	updated, cmd := m.openDiff()
	m = updated.(Model)
	m = settle(t, m, cmdMsg(cmd))
	if m.mode != modeDiff {
		t.Fatalf("mode = %v, want modeDiff", m.mode)
	}
	if m.renderer == nil {
		t.Fatal("no diff on screen")
	}
	// A reviewer is someone with something to say: a diff with no comment
	// against it stays live, so these tests need one to be about holding.
	m.session.Add(review.Comment{File: m.currentFilePath(), StartLine: 1, EndLine: 1, Body: "reviewing"})
	return settle(t, m, cmdMsg(m.rerenderCmd()))
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
	if !m.diffStale() {
		t.Fatal("the diff was not held back")
	}

	m = settle(t, m, key(reloadKey))

	if m.diffStale() {
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
	if !m.diffStale() {
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

	if m.diffStale() {
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
	had := m.session.CountFor("src.ts")
	m = m.cursorTo(t, "DOOMED")
	updated, _ := m.startComment()
	m = updated.(Model)
	m.commentInput.SetValue("this line worries me")
	updated, _ = m.saveComment()
	m = updated.(Model)

	if n := m.session.CountFor("src.ts"); n != had+1 {
		t.Fatalf("the comment was not saved (%d on the file)", n)
	}
	if m.session.StaleCount() != 0 {
		t.Fatal("the comment is stale before anything changed")
	}

	// The agent removes the very line it was about.
	tr.Modify("src.ts", "one\nthree\n")
	m = settle(t, m, m.refreshFilesCmd()())
	m = settle(t, m, key(reloadKey))

	if m.session.CountFor("src.ts") != had+1 {
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
			if !m.diffStale() {
				t.Error("no notice that the diff is out of date")
			}
		})
	}
}

// The hold belongs to the comments. It was once not released with them, which
// left the flag set — and a diff the poll exists to keep live then refused
// every refresh for the rest of the session.
func TestNotify_DeletingTheLastCommentDoesNotFreezeTheDiff(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale() {
		t.Fatal("the diff was not held")
	}

	for _, c := range m.session.CommentsFor("src.ts") {
		m.session.Remove(c.ID)
	}

	// With nothing written against it the diff must go live again.
	tr.Modify("src.ts", "one\nAGAIN\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if got := m.renderer.Content(m.diffCursor); !strings.Contains(got, "AGAIN") {
		t.Errorf("the diff is frozen:\n%s", got)
	}
	if strings.Contains(m.View(), "to reload") {
		t.Error("the notice is still up with nothing held")
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
	if !m.diffStale() {
		t.Fatal("the diff was not held")
	}

	m = settle(t, m, key("J")) // next file

	if m.diffStale() {
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
	if !m.diffStale() {
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
	if !strings.Contains(view, "2 more added") {
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
		if m.diffStale() {
			t.Fatalf("poll %d raised a notice with nothing changed", i+1)
		}
	}
	if strings.Contains(m.View(), "to reload") {
		t.Errorf("a notice appeared with nothing changed:\n%s", m.View())
	}

	// And a change to a *different* file is still not this diff moving.
	tr.Modify("b.ts", "one\nOTHER\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if m.diffStale() {
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

	if m.diffStale() {
		t.Error("a new file elsewhere raised a notice about the diff on screen")
	}

	// And the branch that matters: the file on screen leaving the changeset
	// entirely. There is nothing to reload, so there is nothing to offer.
	tr.Git("checkout", "--", "a.ts")
	m = settle(t, m, m.refreshFilesCmd()())
	if m.diffStale() {
		t.Error("a notice offers to reload a file that is no longer in the changeset")
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
	m.mode = modeDiff
	m.renderer = nil // nothing drawn yet

	tr.Modify("a.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if m.diffStale() {
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
	if !m.diffStale() {
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
	updated, _ := m.updateDiffMode(key("J"))
	m = updated.(Model)
	if m.rendererPath == "" {
		t.Fatal("no renderer path to compare against")
	}
	if m.rendererPath == m.currentFilePath() {
		t.Skip("the diff loaded synchronously here, so there is no window to test")
	}

	// Modify the file the *renderer* is holding, which is not the one under
	// the cursor. Staleness is keyed on the renderer's path, so this is the
	// case the guard exists for: without it the bar would raise a notice about
	// a file the reviewer has already moved away from, and R would reload
	// whatever the cursor is on instead.
	held := m.rendererPath
	tr.Modify(held, "one\nREWRITTEN\n")
	// Deliver the refresh and stop there. settle would run the diff load that
	// n set in motion, which resolves the disagreement before the assertion —
	// so the window this test is about would be gone by the time it looked.
	updated, _ = m.Update(m.refreshFilesCmd()())
	m = updated.(Model)

	if m.diffStale() {
		t.Errorf("a notice was raised about %q while the cursor is on %q",
			held, m.currentFilePath())
	}
	if strings.Contains(m.View(), "to reload") {
		t.Errorf("the bar offers a reload for a file the cursor has left:\n%s", m.View())
	}
}

// The changeset emptying used to leave the notice up for good: the early
// return skipped the clear, and the only thing that cleared it was a diff
// load, which cannot happen with no files. The screen read "Nothing to review"
// and "diff moved — R to reload" at the same time, and R did nothing.
func TestNotify_AnEmptyChangesetLeavesNoNoticeBehind(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale() {
		t.Fatal("the diff was not held")
	}

	// The agent commits everything.
	tr.Git("add", "-A")
	tr.Git("commit", "-m", "done")
	m = settle(t, m, m.refreshFilesCmd()())

	if len(m.files) != 0 {
		t.Fatalf("expected an empty changeset, got %d files", len(m.files))
	}
	if m.diffStale() {
		t.Error("the notice survived the changeset emptying")
	}
	if strings.Contains(m.View(), "to reload") {
		t.Errorf("the screen offers a reload with nothing to reload:\n%s", m.View())
	}
}

// A resize must not perform the content swap the hold exists to prevent — nor
// quietly remove the notice saying it had happened.
func TestNotify_AResizeDoesNotSwapAHeldDiff(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale() {
		t.Fatal("the diff was not held")
	}

	m = settle(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})

	if got := m.renderer.Content(m.diffCursor); strings.Contains(got, "REWRITTEN") {
		t.Errorf("the resize swapped the held diff:\n%s", got)
	}
	if !m.diffStale() {
		t.Error("the resize cleared the notice without the reviewer seeing what moved")
	}
}

// The notice belongs to the key that clears it. Outside the diff R is
// unbound, so offering it there is a wrong instruction.
func TestNotify_TheNoticeIsOnlyOfferedWhereItWorks(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !strings.Contains(m.View(), "to reload") {
		t.Fatal("no notice in review mode")
	}

	for _, mode := range []viewMode{modeFileList, modeCommit, modeBranchPicker} {
		probe := m
		probe.mode = mode
		if strings.Contains(probe.View(), "to reload") {
			t.Errorf("mode %v offers R, which does nothing there", mode)
		}
	}
}

// An ordinary status message comes after the notice — the notice says the
// screen is not showing the repository, and everything else in that row
// describes the screen. A *failure* is the exception and goes first; see
// TestNotify_AFailureIsNotCrowdedOutByTheNotice.
func TestNotify_TheNoticeComesFirstInTheBar(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())
	m.statusMsg = "a status message"

	segment := m.statusSegment()
	notice := strings.Index(segment, "diff moved")
	status := strings.Index(segment, "a status message")
	if notice < 0 || status < 0 {
		t.Fatalf("expected both in the bar, got %q", segment)
	}
	if notice > status {
		t.Errorf("the notice comes after the status message: %q", segment)
	}
}

// The file list, the diff panel and the notice must agree about which file
// they are describing.
//
// m.cursor is an index into m.files, and the hold skips the reload that would
// re-sync the panel — so a file sorting earlier entering the changeset in the
// same refresh shifted every index below it, and the reviewer saw one file
// highlighted, another's content, and a notice about a third. R then reloaded
// the wrong one.
func TestNotify_TheListTheDiffAndTheNoticeAgree(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.CommitFile("aaa.ts", "one\ntwo\n", "second")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	if m.currentFilePath() != "src.ts" {
		t.Fatalf("expected to start on src.ts, got %q", m.currentFilePath())
	}

	// A file sorting before it joins the changeset in the same refresh that
	// rewrites the one on screen.
	tr.Modify("aaa.ts", "one\nNEW ENTRY\n")
	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if m.currentFilePath() != m.rendererPath {
		t.Errorf("the list highlights %q while the panel shows %q",
			m.currentFilePath(), m.rendererPath)
	}
	if m.diffStale() && m.rendererPath != "src.ts" {
		t.Errorf("the notice is about %q, which is not the diff on screen", m.rendererPath)
	}
}

// The same-shape edit, which is the commonest one an agent makes and the one
// that keeps slipping through. A line replaced leaves the added and removed
// counts untouched, so filesEqual is true and the refresh takes the short arm.
// Re-anchoring has to happen there too: it is what marks a comment stale, and
// without it a comment about a line that has already been replaced stays
// pending and sends on the first press, quoting code that is gone.
func TestNotify_AReplacedLineStalesItsCommentToo(t *testing.T) {
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

	before := m.files[m.cursor].change
	// Replaced, not added or removed: the counts do not move.
	tr.Modify("src.ts", "one\nREPLACED\nthree\n")
	m = settle(t, m, m.refreshFilesCmd()())

	after := m.files[m.cursor].change
	if before.AddedLines != after.AddedLines || before.DeletedLines != after.DeletedLines {
		t.Fatalf("this fixture changed the line counts (%d/%d -> %d/%d), so it is "+
			"not testing the short arm",
			before.AddedLines, before.DeletedLines, after.AddedLines, after.DeletedLines)
	}
	if !m.diffStale() {
		t.Fatal("the diff was not held")
	}
	if m.session.StaleCount() != 1 {
		t.Errorf("the comment is still pending while the line it describes is gone: %d stale",
			m.session.StaleCount())
	}
}

// A replacement is invisible to the line counts, so the notice has to say
// something rather than nothing.
func TestNotify_ASameSizeRewriteStillSaysSomething(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Modify("src.ts", "one\nREPLACED\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale() {
		t.Fatal("the diff was not held")
	}

	if got := m.changeSince(m.files); got != "rewritten" {
		t.Errorf("a same-size rewrite is described as %q", got)
	}
	if !strings.Contains(m.View(), "rewritten") {
		t.Errorf("the notice says nothing about what moved:\n%s", m.View())
	}
}

// The agent taking lines back out has to read as English. "-3 added" is not a
// sentence.
func TestNotify_TheSummaryReadsBothWays(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "one\ntwo\nthree\nfour\n")

	m := reviewing(t, tr)
	// The agent reverts two of the three lines it added.
	tr.Modify("src.ts", "one\ntwo\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale() {
		t.Fatal("the diff was not held")
	}

	got := m.changeSince(m.files)
	if strings.Contains(got, "-") {
		t.Errorf("the summary reads %q, which is not English", got)
	}
	if !strings.Contains(got, "fewer") {
		t.Errorf("a partial revert is described as %q", got)
	}
}

// Another file changing is not a reason to send the reviewer back to the top
// of the one they are reading — and with an agent working it is the common
// case, not the rare one.
func TestNotify_AnotherFileChangingKeepsTheReviewersPlace(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	body := strings.Repeat("keep\n", 30)
	tr.CommitFile("src.ts", body+"two\n", "first")
	tr.CommitFile("other.ts", "one\n", "second")
	tr.Modify("src.ts", body+"CHANGED\n")

	m := reviewing(t, tr)
	for i := 0; i < 10; i++ {
		updated, _ := m.updateDiffMode(key("j"))
		m = updated.(Model)
	}
	cursor := m.diffCursor
	if cursor == 0 {
		t.Fatal("the cursor did not move")
	}

	// A different file joins the changeset.
	tr.Modify("other.ts", "one\nTOUCHED BY THE AGENT\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if m.diffCursor != cursor {
		t.Errorf("an edit to another file moved the cursor from %d to %d", cursor, m.diffCursor)
	}
}

// A resize or a theme preview must not un-stale the comments the hold just
// marked. This is the round-2 defect arriving through a new door: the
// re-render rebuilds from the *held* parse, which still contains the line the
// agent deleted — so re-anchoring against it restored every stale comment, and
// #44 would then send one on the first press quoting code that is gone.
func TestNotify_ARerenderDoesNotUnstaleComments(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		jog  func(t *testing.T, m Model) Model
	}{
		{"a resize", func(t *testing.T, m Model) Model {
			return settle(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
		}},
		{"a theme preview", func(t *testing.T, m Model) Model {
			mm, _ := m.openThemePicker()
			mm, cmd := mm.moveThemeCursor(1)
			return settle(t, mm, cmdMsg(cmd))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
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

			tr.Modify("src.ts", "one\nthree\n")
			m = settle(t, m, m.refreshFilesCmd()())
			if m.session.StaleCount() != 1 {
				t.Fatalf("the comment is not stale to begin with: %d", m.session.StaleCount())
			}

			m = tc.jog(t, m)

			if m.session.StaleCount() != 1 {
				t.Errorf("%s restored the comment to pending: %d stale",
					tc.name, m.session.StaleCount())
			}
		})
	}
}

// Navigating away must not be undone by a refresh that lands before the new
// diff does.
func TestNotify_ARefreshDoesNotUndoNavigation(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "first")
	tr.CommitFile("b.ts", "one\ntwo\n", "second")
	tr.Modify("a.ts", "one\nCHANGED\n")
	tr.Modify("b.ts", "one\nOTHER\n")

	m := reviewing(t, tr)
	start := m.currentFilePath()

	// n, without letting the new diff land.
	updated, _ := m.updateDiffMode(key("J"))
	m = updated.(Model)
	asked := m.currentFilePath()
	if asked == start {
		t.Fatal("n did not move to another file")
	}

	// A refresh arrives in that window.
	updated, _ = m.Update(m.refreshFilesCmd()())
	m = updated.(Model)

	if got := m.currentFilePath(); got != asked {
		t.Errorf("the refresh dragged the cursor from %q back to %q", asked, got)
	}
}

// A path with both staged and unstaged changes is two entries. The cursor has
// to stay on the half it was on: loadDiffCmd reads Staged from the entry, so
// landing on the other one makes R swap in a different diff.
func TestNotify_TheCursorKeepsItsHalfOfADualEntryFile(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nSTAGED\n")
	tr.Stage("src.ts")
	tr.Modify("src.ts", "one\nSTAGED\nUNSTAGED\n")

	m := reviewing(t, tr)
	staged, unstaged := -1, -1
	for i, f := range m.files {
		if f.change.Path != "src.ts" {
			continue
		}
		if f.change.Staged {
			staged = i
		} else {
			unstaged = i
		}
	}
	if staged < 0 || unstaged < 0 {
		t.Skipf("this fixture did not produce both halves (staged=%d unstaged=%d)", staged, unstaged)
	}

	m.cursor = unstaged
	m = settle(t, m, m.loadDiffCmd(true))

	// Another file joins, reordering the list.
	tr.Untracked("aaa.ts", "new\n")
	updated, _ := m.Update(m.refreshFilesCmd()())
	m = updated.(Model)

	if m.cursor >= len(m.files) {
		t.Fatalf("cursor %d is outside a list of %d", m.cursor, len(m.files))
	}
	got := m.files[m.cursor]
	if got.change.Path != "src.ts" || got.change.Staged {
		t.Errorf("the cursor moved to %q (staged=%v); it was on the unstaged half of src.ts",
			got.change.Path, got.change.Staged)
	}
}

// A re-render has to reproduce what a load would build, not an approximation.
// Tab width and split mode were simply dropped, and the split rule was missing
// its onePanel clause — so resizing into the 60-71 column band engaged split
// view where a fresh load refuses.
func TestNotify_ARerenderBuildsWhatALoadWouldHaveBuilt(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n\ttabbed\nthree\n", "first")
	tr.Modify("src.ts", "one\n\tCHANGED\nthree\n")

	for _, width := range []int{65, 80, 120, 200} {
		m := reviewing(t, tr)
		m.splitDiff = true
		m = settle(t, m, tea.WindowSizeMsg{Width: width, Height: 30})

		fresh, ok := m.loadDiffCmd(false)().(diffLoadedMsg)
		if !ok || fresh.renderer == nil {
			t.Fatalf("%d: no renderer from a fresh load", width)
		}
		again, ok := m.rerenderCmd()().(diffLoadedMsg)
		if !ok || again.renderer == nil {
			t.Fatalf("%d: no renderer from a re-render", width)
		}

		if fresh.renderer.split != again.renderer.split {
			t.Errorf("%d cols: a load builds split=%v, a re-render builds split=%v",
				width, fresh.renderer.split, again.renderer.split)
		}
		if fresh.renderer.Content(-1) != again.renderer.Content(-1) {
			t.Errorf("%d cols: a re-render does not match what a load builds", width)
		}
		if again.resetScroll {
			t.Errorf("%d cols: a re-render would send the reviewer back to the top", width)
		}
	}
}

// A late re-render must not revert an explicit reload. Press R, then resize
// before it lands: the re-render carries the held content, so arriving last it
// would undo the reload and put the notice back.
func TestNotify_ALateRerenderDoesNotRevertAReload(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale() {
		t.Fatal("the diff was not held")
	}

	// Both commands are taken while stale, so the re-render is the older.
	stale := m.rerenderCmd()()
	updated, reload := m.reloadDiff()
	m = updated.(Model)
	if reload == nil {
		t.Fatal("R scheduled nothing")
	}

	// The reload lands, then the re-render arrives late.
	m = settle(t, m, reload())
	if got := m.renderer.Content(m.diffCursor); !strings.Contains(got, "REWRITTEN") {
		t.Fatalf("the reload did not install the new content:\n%s", got)
	}
	updated, _ = m.Update(stale)
	m = updated.(Model)

	if got := m.renderer.Content(m.diffCursor); !strings.Contains(got, "REWRITTEN") {
		t.Errorf("a late re-render reverted the reload:\n%s", got)
	}
	if m.diffStale() {
		t.Error("the notice came back after an explicit reload")
	}
}

// Outside review mode a resize must load, not re-render: R is unbound there,
// so keeping old content would be a freeze with no way out.
func TestNotify_AResizeOutsideReviewAlwaysLoads(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m = settle(t, m, m.refreshFilesCmd()())
	m = settle(t, m, key("enter"))
	if m.mode != modeDiff {
		t.Fatalf("mode = %v, want modeDiff", m.mode)
	}

	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())
	m = settle(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	if got := m.renderer.Content(m.diffCursor); !strings.Contains(got, "REWRITTEN") {
		t.Errorf("the plain diff kept old content across a resize:\n%s", got)
	}
}

// The key the renderer records has to come from the content it read, not from
// the previous poll's map — otherwise a write landing between the two makes
// the next poll announce a change that is already on screen.
func TestNotify_TheRecordedKeyComesFromTheContentRead(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	// A write lands after the last refresh but before this load.
	tr.Modify("src.ts", "one\nNEWER\n")
	m = settle(t, m, m.loadDiffCmd(false)())

	if got := m.renderer.Content(m.diffCursor); !strings.Contains(got, "NEWER") {
		t.Fatalf("the load did not read the newer content:\n%s", got)
	}
	// The next poll sees the same content the renderer holds, so there is
	// nothing to announce.
	m = settle(t, m, m.refreshFilesCmd()())
	if m.diffStale() {
		t.Error("the notice fired for content that is already on screen")
	}
}

// noteChangedFiles must never adopt an empty key map: doing so makes every
// file look new on the next refresh and switches detection off for the run.
func TestNotify_ANilKeyMapIsNotAdopted(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	before := m.fileKeys
	if len(before) == 0 {
		t.Fatal("no keys to begin with")
	}

	m = m.noteChangedFiles(nil, false)
	if len(m.fileKeys) != len(before) {
		t.Errorf("a nil key map replaced %d keys with %d", len(before), len(m.fileKeys))
	}
}

// Previewing a theme must not swap a held diff, for the same reason a resize
// must not. Staleness alone cannot see this: a fresh load re-anchors against
// the new content and the comment stays stale either way, so the thing to
// assert is the content on screen.
func TestNotify_AThemePreviewDoesNotSwapAHeldDiff(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale() {
		t.Fatal("the diff was not held")
	}

	mm, _ := m.openThemePicker()
	mm, cmd := mm.moveThemeCursor(1)
	m = settle(t, mm, cmdMsg(cmd))

	if got := m.renderer.Content(m.diffCursor); strings.Contains(got, "REWRITTEN") {
		t.Errorf("the theme preview swapped the held diff:\n%s", got)
	}
	if !m.diffStale() {
		t.Error("the preview cleared the notice without the reviewer seeing what moved")
	}
}

// The summary totals both halves of a dual-entry file. Taking the first match
// compared one half against itself, so adding a line to the other half
// reported "rewritten" when it had plainly grown.
func TestNotify_TheSummaryTotalsBothHalvesOfAFile(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nSTAGED\n")
	tr.Stage("src.ts")

	m := reviewing(t, tr)
	staged, unstaged := 0, 0
	for _, f := range m.files {
		if f.change.Path != "src.ts" {
			continue
		}
		staged += f.change.AddedLines
		unstaged++
	}
	if unstaged == 0 {
		t.Skip("fixture produced no src.ts entry")
	}

	// A line added to the worktree half, on top of the staged change.
	tr.Modify("src.ts", "one\nSTAGED\nUNSTAGED\n")
	m = settle(t, m, m.refreshFilesCmd()())

	if !m.diffStale() {
		t.Skip("this fixture did not hold the diff")
	}
	if got := m.changeSince(m.files); got == "rewritten" {
		t.Errorf("a line was added and the summary says %q — one half was "+
			"compared against itself", got)
	}
}

// The hold belongs to the diff on screen. Without that guard, a refresh
// arriving after the user navigated would hold on the *renderer's* file and
// skip the reload for the file they asked for — so the panel never catches up.
func TestNotify_NavigatingStillLoadsTheFileAskedFor(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "first")
	tr.CommitFile("b.ts", "one\ntwo\n", "second")
	tr.Modify("a.ts", "one\nCHANGED\n")
	tr.Modify("b.ts", "one\nOTHER\n")

	m := reviewing(t, tr)
	start := m.rendererPath

	// Navigate, and let a refresh land in the window before the diff does —
	// with the file we navigated *away* from having changed, which is what
	// makes an ungated hold engage.
	updated, nav := m.updateDiffMode(key("J"))
	m = updated.(Model)
	asked := m.currentFilePath()
	tr.Modify(start, "one\nREWRITTEN\n")
	updated, _ = m.Update(m.refreshFilesCmd()())
	m = updated.(Model)

	// The navigation's own load now lands.
	m = settle(t, m, cmdMsg(nav))

	if m.rendererPath != asked {
		t.Errorf("the panel shows %q; the reviewer asked for %q", m.rendererPath, asked)
	}
	if got := m.renderer.Content(m.diffCursor); !strings.Contains(got, "OTHER") {
		t.Errorf("the panel did not load the file asked for:\n%s", got)
	}
}

// Staging has to show up at once.
//
// buildRefreshedFiles left its sequence number at zero, and the out-of-order
// guard drops anything older than what is installed — so once a single
// probe-driven refresh had landed, every `tab` and `a` refresh was thrown
// away. The stage itself succeeded; the list just did not move until the next
// probe noticed, turning the most-pressed key in the file list from instant
// into a one-to-two-second lag. Nothing caught it because the tests that
// exercise toggleStage never let a probe refresh land first.
func TestRefresh_StagingShowsUpWithoutWaitingForAProbe(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")

	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})

	// A probe refresh lands first, which is what installs a sequence number.
	m = settle(t, m, m.probeCmd()())
	if m.installedSeq == 0 {
		t.Fatal("no probe refresh landed, so this test proves nothing")
	}
	if m.files[m.cursor].change.Staged {
		t.Fatal("the file is already staged")
	}

	// tab, and the refresh it schedules.
	updated, cmd := m.toggleStage()
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("tab scheduled no refresh")
	}
	for _, msg := range fanOut(cmd) {
		updated, _ = m.Update(msg)
		m = updated.(Model)
	}

	staged := false
	for _, f := range m.files {
		if f.change.Path == "src.ts" && f.change.Staged {
			staged = true
		}
	}
	if !staged {
		t.Error("the list still shows src.ts unstaged after tab")
	}
}

// Every refresh gets a sequence of its own. The explicit ones reused whatever
// number the last probe had, so neither was older by the guard's test and the
// last to land won — stale fingerprint included, which is the state the guard
// exists to prevent.
func TestRefresh_EveryRefreshGetsItsOwnSequence(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})

	seen := map[int]bool{}
	for i := 0; i < 4; i++ {
		cmd := m.nextRefresh()
		msg, ok := cmd().(filesRefreshedMsg)
		if !ok {
			t.Fatalf("refresh %d produced no filesRefreshedMsg", i)
		}
		if msg.seq == 0 {
			t.Errorf("refresh %d was stamped 0, which the guard treats as oldest", i)
		}
		if seen[msg.seq] {
			t.Errorf("sequence %d was issued twice", msg.seq)
		}
		seen[msg.seq] = true
	}
}

// View must not shell out to git.
//
// renderHeader called BranchName(), which is a synchronous `git rev-parse`
// — so every rendered frame started a process and blocked on it, roughly one
// per keypress. That is also why #45's "idle sessions issue approximately no
// git subprocesses" did not hold: the probe was one process a tick and the
// header was two more, or four on a detached HEAD.
func TestView_StartsNoProcesses(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")

	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	// Take the repo away. Anything in View that needs git will now say so
	// rather than quietly starting a process.
	m.repo = nil

	before := m.View()
	if !strings.Contains(before, tr.Git("rev-parse", "--abbrev-ref", "HEAD")) {
		t.Errorf("the header does not name the branch without asking git:\n%s", before)
	}
	// And it must be stable: rendering twice cannot depend on anything
	// outside the model.
	if after := m.View(); after != before {
		t.Error("two renders of the same model disagree")
	}
}

// The header reads the branch off the model, so every path that changes the
// branch has to keep it current — otherwise the header names the old one
// indefinitely.
func TestView_TheHeaderFollowsABranchChange(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	start := m.branchName()
	if start == "" {
		t.Fatal("no branch to begin with")
	}

	// Switched under us, the way the branch picker does it.
	tr.Git("checkout", "-q", "-b", "elsewhere")
	m = settle(t, m, branchSwitchedMsg{})
	if got := m.branchName(); got != "elsewhere" {
		t.Errorf("after a switch the header says %q, want %q", got, "elsewhere")
	}
	if !strings.Contains(m.View(), "elsewhere") {
		t.Errorf("the header does not show the new branch:\n%s", m.View())
	}

	// And a created one.
	m = settle(t, m, branchCreatedMsg{name: "fresh"})
	if got := m.branchName(); got != "fresh" {
		t.Errorf("after a create the header says %q, want %q", got, "fresh")
	}
}

// A diff load must be matched to the file it was read for, not to an index
// into a list that the next refresh replaces.
//
// diffLoadedMsg carried only index, so: reviewer on b.ts with a pending
// comment, presses n (cursor 0→1, load for c.ts in flight), a refresh lands
// carrying a newly-modified a.ts which sorts first. The cursor stays at 1,
// which is now b.ts, and the in-flight load arrives with index == cursor and
// is installed — so the panel shows c.ts's diff labelled b.ts, b.ts's comments
// are drawn on c.ts's rows, and Reanchor runs b.ts's comments against c.ts's
// parse and marks a perfectly valid one stale.
func TestNotify_ALoadIsMatchedToItsFileNotItsIndex(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	for _, n := range []string{"b.ts", "c.ts", "d.ts"} {
		tr.CommitFile(n, "one\ntwo\n", "add "+n)
	}
	tr.CommitFile("a.ts", "one\ntwo\n", "add a")
	for _, n := range []string{"b.ts", "c.ts", "d.ts"} {
		tr.Modify(n, "one\n"+strings.ToUpper(n)+" CHANGED\n")
	}

	m := reviewing(t, tr)
	for m.currentFilePath() != "b.ts" {
		updated, _ := m.updateDiffMode(key("J"))
		mm := updated.(Model)
		if mm.currentFilePath() == m.currentFilePath() {
			t.Fatal("could not reach b.ts")
		}
		m = settle(t, mm, nil)
		m = settle(t, m, m.loadDiffCmd(true)())
	}
	m = m.cursorTo(t, "B.TS CHANGED")
	updated, _ := m.startComment()
	m = updated.(Model)
	m.commentInput.SetValue("a comment about b.ts")
	updated, _ = m.saveComment()
	m = updated.(Model)
	if m.session.StaleCount() != 0 {
		t.Fatal("the comment is stale already")
	}

	t.Logf("after saving: stale=%d cursor=%d path=%q renderer=%q",
		m.session.StaleCount(), m.cursor, m.currentFilePath(), m.rendererPath)

	// n, keeping the load in flight.
	updated, nav := m.updateDiffMode(key("J"))
	m = updated.(Model)
	pending := cmdMsg(nav)
	t.Logf("after n:  stale=%d cursor=%d path=%q renderer=%q",
		m.session.StaleCount(), m.cursor, m.currentFilePath(), m.rendererPath)

	// a.ts joins the changeset and sorts before everything.
	tr.Modify("a.ts", "one\nA CHANGED\n")
	updated, _ = m.Update(m.refreshFilesCmd()())
	m = updated.(Model)
	t.Logf("after refresh: stale=%d cursor=%d path=%q renderer=%q",
		m.session.StaleCount(), m.cursor, m.currentFilePath(), m.rendererPath)

	// The in-flight load lands.
	updated, _ = m.Update(pending)
	m = updated.(Model)

	if m.rendererPath != m.currentFilePath() {
		t.Errorf("the panel shows %q while the list highlights %q",
			m.rendererPath, m.currentFilePath())
	}
	if m.session.StaleCount() != 0 {
		t.Errorf("a valid comment on b.ts was marked stale: %d stale",
			m.session.StaleCount())
	}
}

// The count must not lie about the file the reviewer is on.
//
// noteChangedFiles excluded the cursor's path, on the reasoning that "whatever
// arrives for it is what the user is looking at, so it cannot be out of date
// to its own reader". #46 made that false: the diff is held, so the file on
// screen *is* out of date to its reader — and nothing marked it. Read a file,
// have the agent rewrite it, move on without reloading, and it still counted
// as reviewed.
func TestNotify_TheFileOnScreenIsMarkedChangedWhenItIsHeld(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "first")
	tr.CommitFile("b.ts", "one\ntwo\n", "second")
	tr.Modify("a.ts", "one\nCHANGED\n")
	tr.Modify("b.ts", "one\nOTHER\n")

	m := reviewing(t, tr)
	read := m.currentFilePath()
	before := m.reviewProgress()
	if before.Reviewed == 0 {
		t.Fatal("the file on screen does not count as read to begin with")
	}

	// The agent rewrites the file being read. The diff is held.
	tr.Modify(read, "one\nREWRITTEN\nAND LONGER\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale() {
		t.Fatal("the diff was not held, so this is not the case under test")
	}

	after := m.reviewProgress()
	if after.Changed == 0 {
		t.Errorf("the file was rewritten under the reviewer and nothing marked it: "+
			"%d/%d reviewed, %d changed", after.Reviewed, after.Total, after.Changed)
	}
	if after.Reviewed >= before.Reviewed {
		t.Errorf("the ratio did not move: %d/%d before, %d/%d after",
			before.Reviewed, before.Total, after.Reviewed, after.Total)
	}
}

// And leaving the file without reloading must not launder the signal away.
func TestNotify_MovingOnDoesNotClearAnUnreloadedChange(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "first")
	tr.CommitFile("b.ts", "one\ntwo\n", "second")
	tr.Modify("a.ts", "one\nCHANGED\n")
	tr.Modify("b.ts", "one\nOTHER\n")

	m := reviewing(t, tr)
	read := m.currentFilePath()
	tr.Modify(read, "one\nREWRITTEN\nAND LONGER\n")
	m = settle(t, m, m.refreshFilesCmd()())

	// n, without pressing R.
	m = settle(t, m, key("J"))
	if m.currentFilePath() == read {
		t.Fatal("n did not move off the file")
	}

	if got := m.session.ChangedSinceViewed(read); !got {
		t.Errorf("%q was rewritten unread and moving on cleared the mark", read)
	}
}

// The bar must not offer a key that does nothing where it is shown.
//
// `!` is answered outside the typing guard, so in the branch picker, the
// branch-name input, the commit input and the comment editor it goes into the
// text field. A duplicate-branch failure was telling the user to press it
// while it typed an exclamation mark into the filter.
func TestProblem_DetailsAreOnlyOfferedWhereTheKeyWorks(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")
	base := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})

	boom := errors.New("fatal: a branch named 'x' already exists")

	for _, tc := range []struct {
		name   string
		typing bool
		set    func(m Model) Model
	}{
		{"file list", false, func(m Model) Model { m.mode = modeFileList; return m }},
		{"diff", false, func(m Model) Model { m.mode = modeDiff; return m }},
		{"branch picker", true, func(m Model) Model { m.mode = modeBranchPicker; return m }},
		{"branch create", true, func(m Model) Model {
			m.mode = modeBranchPicker
			m.branchCreating = true
			return m
		}},
		{"commit input", true, func(m Model) Model { m.mode = modeCommit; return m }},
		{"comment editor", true, func(m Model) Model { m.commenting = true; return m }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := tc.set(base)
			m = m.fail("creating the branch", boom)
			if m.problem == nil {
				t.Fatal("the failure was not retained")
			}
			offered := strings.Contains(m.statusMsg, "! details")
			if offered == tc.typing {
				t.Errorf("typing=%v but the bar %s offer ! details: %q",
					tc.typing, map[bool]string{true: "does", false: "does not"}[offered], m.statusMsg)
			}
		})
	}
}

// Toggling split view must not swap a held diff either. handleResize and
// applyTheme both re-render for this reason; v did a full re-read from disk
// and took the notice with it.
func TestNotify_SplitToggleDoesNotSwapAHeldDiff(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := reviewing(t, tr)
	tr.Modify("src.ts", "one\nREWRITTEN\n")
	m = settle(t, m, m.refreshFilesCmd()())
	if !m.diffStale() {
		t.Fatal("the diff was not held")
	}

	updated, cmd := m.updateDiffMode(key("v"))
	m = settle(t, updated.(Model), cmdMsg(cmd))

	if got := m.renderer.Content(m.diffCursor); strings.Contains(got, "REWRITTEN") {
		t.Errorf("v swapped the held diff:\n%s", got)
	}
	if !m.diffStale() {
		t.Error("v cleared the notice without the reviewer seeing what moved")
	}
}

// Editor failures go through fail, like everything else. They were assigned to
// statusMsg raw, so a tmux error — which internal/editor appends stderr to on
// purpose — arrived in the one-line bar verbatim and `!` said nothing had gone
// wrong.
func TestProblem_EditorFailuresGoThroughFail(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})

	raw := errors.New("tmux list-panes -a -F #{pane_id}: exit status 1:\nno server running\non /tmp/tmux-501/default")
	updated, _ := m.Update(editorPlanMsg{err: raw})
	m = updated.(Model)

	if m.problem == nil {
		t.Fatal("the failure was not retained, so ! would say nothing went wrong")
	}
	if strings.Contains(m.statusMsg, "\n") {
		t.Errorf("a multi-line error went into the one-line bar: %q", m.statusMsg)
	}
	if !strings.Contains(m.statusMsg, "editor") {
		t.Errorf("the bar does not say what differ was doing: %q", m.statusMsg)
	}
}

// A failure must survive the notice. The notice is up to 56 columns and was
// placed ahead of statusMsg, then the joined row was cut to the terminal — so
// at eighty columns a failed send lost its "! details" and below seventy-two
// it was invisible. A failure is the one thing in this row that has to be
// acted on.
func TestNotify_AFailureIsNotCrowdedOutByTheNotice(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\nthree\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\nthree\n")

	for i, width := range []int{40, 50, 60, 72, 80, 100, 120} {
		m := reviewing(t, tr)
		m = settle(t, m, tea.WindowSizeMsg{Width: width, Height: 30})
		// Different content each time: the repository is shared across the
		// loop, so rewriting it to the same thing twice leaves the second
		// iteration with nothing to notice.
		tr.Modify("src.ts", "one\nREWRITTEN "+strings.Repeat("x", i+1)+"\nAND MORE\nthree\n")
		m = settle(t, m, m.refreshFilesCmd()())
		if !m.diffStale() {
			t.Fatalf("%d: the diff was not held", width)
		}
		m = m.fail("sending", errors.New("tmux: no server running"))

		segment := m.statusSegment()
		if !strings.Contains(segment, "sending failed") {
			t.Errorf("%d cols: the failure is not in the bar: %q", width, segment)
		}
	}
}
