package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/config"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
	"github.com/jansmrcka/differ/internal/theme"
)

// liveReview is a review of a real repository, started the way cmd/root.go
// starts one. Two of them in a row are a restart: the second gets nothing from
// the first except what reached the disk.
func liveReview(t *testing.T, tr *testutil.Repo) Model {
	t.Helper()
	return started(t, tr, true)
}

// restart is the same, stopping short of review mode, which is how a file's
// state can be read without this run's own reading counting towards it.
func restart(t *testing.T, tr *testutil.Repo) Model {
	t.Helper()
	return started(t, tr, false)
}

func started(t *testing.T, tr *testutil.Repo, reviewing bool) Model {
	t.Helper()
	repo, err := git.NewRepo(tr.Dir)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := repo.ChangedFiles(false, "")
	if err != nil {
		t.Fatal(err)
	}
	untracked, err := repo.UntrackedFiles()
	if err != nil {
		t.Fatal(err)
	}
	th := theme.Themes["dark"]
	m := NewModel(repo, config.Default(), changes, untracked, NewStyles(th), th, false, "")
	if reviewing {
		m.StartInReviewMode()
	}

	// The resize is what loads the first diff, and settle follows it. Init is
	// deliberately not run here: its tick is a one-second timer.
	return settle(t, m, tea.WindowSizeMsg{Width: 120, Height: 32})
}

// writeComment does what a reviewer does: put the cursor on a line, press c,
// type, and save.
func writeComment(t *testing.T, m Model, line, body string) Model {
	t.Helper()
	m = cursorOn(t, m, LineAdded, line)
	updated, _ := m.updateReviewMode(key("c"))
	m = typeText(t, updated.(Model), body)
	updated, _ = m.updateReviewMode(key("ctrl+s"))
	m = updated.(Model)
	if m.commenting {
		t.Fatalf("the editor is still open: %s", m.statusMsg)
	}
	return m
}

func TestPersist_ACommentSurvivesTheProcessEnding(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))

	first := liveReview(t, tr)
	first = writeComment(t, first, "  const user = await getUser(id)", "why await here?")
	if n := first.session.CountFor("src.ts"); n != 1 {
		t.Fatalf("the comment was not written in the first place: %d on src.ts", n)
	}

	// No quit, no flush: a kill is the case this exists for.
	second := liveReview(t, tr)

	if second.session == nil {
		t.Fatal("the second run has no session at all")
	}
	got := second.session.CommentsFor("src.ts")
	if len(got) != 1 {
		t.Fatalf("restored %d comments, want 1", len(got))
	}
	if got[0].Body != "why await here?" {
		t.Errorf("restored body = %q", got[0].Body)
	}
	if got[0].StartLine != 2 {
		t.Errorf("restored comment is on line %d, want 2", got[0].StartLine)
	}
	if got[0].State != review.StatePending {
		t.Errorf("restored comment State = %v, want pending", got[0].State)
	}
}

// Where it goes: inside the repository's own git directory, which is never
// committed and needs no ignore rule. Nothing lands in the working tree.
func TestPersist_StateLivesInTheGitDirectoryAndNotTheWorkingTree(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))

	before := strings.Join(tr.Status(), "\n")

	// The model the comment leaves behind is of no interest here; the point
	// is what writing it put on disk, and where.
	writeComment(t, liveReview(t, tr), "  const user = await getUser(id)", "a note")

	if _, err := os.Stat(filepath.Join(tr.Dir, ".git", "differ", "review.json")); err != nil {
		t.Fatalf("no review state in the git directory: %v", err)
	}
	// git's own answer to "what changed here?" is the test that matters: a
	// file differ left in the working tree would show up as untracked, and
	// would then need an ignore rule to stay out of the way.
	if after := strings.Join(tr.Status(), "\n"); after != before {
		t.Errorf("writing the review changed what git sees:\nbefore: %q\nafter:  %q", before, after)
	}
}

// The case the content key exists for. The comment describes code the agent
// has since replaced, so it is worse than no comment at all — and the file has
// to be read again, which is what unreviewed means.
func TestPersist_AFileRewrittenWhileDifferWasClosedComesBackUnreviewed(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))

	writeComment(t, liveReview(t, tr), "  const user = await getUser(id)", "why await here?")

	tr.ExternalEdit("src.ts", "export const rewritten = true\n")

	// Not entered into review: "unreviewed" is a claim about what the last
	// run left behind, and opening the file again would make it viewed for
	// reasons that have nothing to do with what was restored.
	second := restart(t, tr)

	// Asserted whether or not a session came back. Guarding the whole body
	// with `if second.session != nil` made this test vacuous: everything was
	// dropped, so Load returned nil, and neither assertion had ever run.
	if n := countFor(second.session, "src.ts"); n != 0 {
		t.Errorf("src.ts came back with %d comments, want none", n)
	}
	if got := stateOf(second.session, "src.ts"); got != review.FileUnreviewed {
		t.Errorf("src.ts state = %v, want unreviewed", got)
	}
}

// countFor is CountFor for a session that may not exist. Nothing restored is
// nothing restored for this file either, and saying so here is what lets the
// caller assert instead of skipping.
func countFor(s *review.Session, path string) int {
	if s == nil {
		return 0
	}
	return s.CountFor(path)
}

// stateOf is FileStateOf for a session that may not exist.
func stateOf(s *review.Session, path string) review.FileState {
	if s == nil {
		return review.FileUnreviewed
	}
	return s.FileStateOf(path)
}

// A comment on a file nobody touched comes back on its line, not on the line
// number it happened to have when it was written.
func TestPersist_ACommentFollowsItsLineAcrossARestart(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))

	first := liveReview(t, tr)
	first = writeComment(t, first, "  const user = await getUser(id)", "why await here?")
	written := first.session.CommentsFor("src.ts")[0]

	second := liveReview(t, tr)
	got := second.session.CommentsFor("src.ts")
	if len(got) != 1 {
		t.Fatalf("restored %d comments, want 1", len(got))
	}
	if got[0].StartLine != written.StartLine || got[0].Anchor != written.Anchor {
		t.Errorf("restored at line %d anchored to %q, want %d / %q",
			got[0].StartLine, got[0].Anchor, written.StartLine, written.Anchor)
	}
	if got[0].State != review.StatePending {
		t.Errorf("restored State = %v, want pending — the line is still there", got[0].State)
	}
}

// H answers "what have I already told the agent?". A restart is exactly when
// that question gets asked, so the answer has to outlive the process — and it
// does not depend on the files still being what they were.
func TestPersist_TheDeliveryHistorySurvivesARestart(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))

	first := liveReview(t, tr)
	first = writeComment(t, first, "  const user = await getUser(id)", "why await here?")
	sent := first.session.CommentsFor("src.ts")[0]
	updated, _ := first.handleFeedbackSent(feedbackSentMsg{ids: []string{sent.ID}, target: "tmux"})
	first = updated.(Model)

	// Rewritten, so the comment itself cannot come back: only the record of
	// having sent it.
	tr.ExternalEdit("src.ts", "export const rewritten = true\n")

	second := liveReview(t, tr)
	if second.session == nil {
		t.Fatal("the history did not survive: no session at all")
	}
	h := second.session.History()
	if len(h) != 1 {
		t.Fatalf("restored %d deliveries, want 1", len(h))
	}
	if h[0].Target != "tmux" || len(h[0].Comments) != 1 {
		t.Errorf("restored delivery = %+v", h[0])
	}
	if n := second.session.CountFor("src.ts"); n != 0 {
		t.Errorf("a sent comment came back as %d live comments, want 0", n)
	}
	second.showHistory = true
	if view := second.View(); !strings.Contains(view, "tmux") {
		t.Errorf("H does not show what was sent before the restart:\n%s", view)
	}
}

// Every change, not just the first: the file on disk is the session, so an
// edit or a deletion that never reached it would come back undone.
func TestPersist_EveryChangeToACommentIsWrittenOut(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))

	const line = "  const user = await getUser(id)"
	m := liveReview(t, tr)
	m = writeComment(t, m, line, "first thought")
	m = writeComment(t, m, line, " — second thought")

	after := liveReview(t, tr)
	got := after.session.CommentsFor("src.ts")
	if len(got) != 1 {
		t.Fatalf("restored %d comments, want 1", len(got))
	}
	if got[0].Body != "first thought — second thought" {
		t.Errorf("restored body = %q, want the edited one", got[0].Body)
	}

	m = cursorOn(t, m, LineAdded, line)
	if _, cmd := m.updateReviewMode(key("x")); cmd != nil {
		t.Fatalf("deleting a comment should not need a command")
	}

	gone := liveReview(t, tr)
	if gone.session != nil && gone.session.CountFor("src.ts") != 0 {
		t.Errorf("a deleted comment came back: %d on src.ts", gone.session.CountFor("src.ts"))
	}
}

// onFile puts the cursor on a path and loads its diff.
func onFile(t *testing.T, m Model, path string) Model {
	t.Helper()
	for i, f := range m.files {
		if f.change.Path == path {
			m.cursor = i
		}
	}
	if m.currentFilePath() != path {
		t.Fatalf("%s is not in the changeset", path)
	}
	return settle(t, m, m.loadDiffCmd(true)())
}

// initMessages runs what Init hands the runtime and collects the results.
//
// The commands are run at once rather than in turn because one of them is the
// poll's first tick, which is a one-second timer: waiting for it in sequence
// would make every message behind it a second late.
func initMessages(t *testing.T, m Model) []tea.Msg {
	t.Helper()
	batch, ok := m.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatal("Init did not return a batch of commands")
	}
	ch := make(chan tea.Msg)
	sent := 0
	for _, c := range batch {
		if c == nil {
			continue
		}
		sent++
		go func(c tea.Cmd) { ch <- c() }(c)
	}
	var out []tea.Msg
	giveUp := time.After(5 * time.Second)
	for range sent {
		select {
		case msg := <-ch:
			out = append(out, msg)
		case <-giveUp:
			t.Fatal("a command Init returned never answered")
		}
	}
	return out
}

// A restored comment on a file that is not the one on screen is only reached
// from Init. The file on screen re-anchors when its diff lands; everything
// else would keep the line numbers it was saved with, and be delivered
// quoting a place that no longer says that.
func TestPersist_RestoredCommentsOnOtherFilesAreReanchoredAtStartup(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "a1\na2\na3\n", "init a")
	tr.CommitFile("b.ts", "b1\nb2\nb3\n", "init b")
	tr.ExternalEdit("a.ts", "a1\nchanged a\na3\n")
	tr.ExternalEdit("b.ts", "b1\nchanged b\nb3\n")

	writeComment(t, onFile(t, liveReview(t, tr), "b.ts"), "changed b", "about b")

	// The agent commits b.ts while differ is closed. The file on disk is
	// untouched, so the comment survives the content key — but b.ts is no
	// longer part of the diff, and the comment no longer describes a line
	// anyone is being shown.
	tr.Git("add", "b.ts")
	tr.Git("commit", "-m", "the agent's own commit")

	second := liveReview(t, tr)
	if second.session == nil || second.session.CountFor("b.ts") != 1 {
		t.Fatalf("b.ts's comment did not survive the restart")
	}
	if got := second.session.CommentsFor("b.ts")[0].State; got != review.StatePending {
		t.Fatalf("restored State = %v, want pending before anything re-anchors it", got)
	}

	for _, msg := range initMessages(t, second) {
		if _, ok := msg.(reanchorMsg); ok {
			second = settle(t, second, msg)
		}
	}

	if got := second.session.CommentsFor("b.ts")[0].State; got != review.StateStale {
		t.Errorf("b.ts's comment State = %v, want stale — its line is not in any diff now", got)
	}
}

// Believing your comments are safe when they are not is worse than being told
// on every one of them, so a review that cannot be written says so rather than
// failing quietly.
func TestPersist_AReviewThatCannotBeWrittenSaysSo(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.ApplyFixture(testutil.Fixture(t, "multi_hunk"))
	// A file where the directory has to go. Nothing differ can do with that,
	// and nothing it should hide.
	if err := os.WriteFile(filepath.Join(tr.Dir, ".git", "differ"), []byte("in the way\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := liveReview(t, tr)
	m = writeComment(t, m, "  const user = await getUser(id)", "a note")

	if !strings.Contains(m.statusMsg, "saving the review failed") {
		t.Errorf("status = %q, want it to say the review could not be saved", m.statusMsg)
	}
	// And the comment is still in the session: a failed write costs the
	// safety net, not the comment.
	if n := m.session.CountFor("src.ts"); n != 1 {
		t.Errorf("the comment was lost with the write: %d on src.ts", n)
	}
}

// A comment must survive differ being reopened in another mode.
//
// The key that decides "has this file moved" was the mode-dependent one:
// a worktree hash normally, a staged object id under -s. So a review written
// with `differ` and reopened with `differ -s` found no comparable key, dropped
// every pending comment, and then overwrote the file on the next change —
// losing them for good rather than merely not showing them.
//
// The question persistence asks is not the one change detection asks. Change
// detection asks whether the diff on screen is out of date, which depends on
// what is being diffed. Persistence asks whether the file the reviewer read
// has been re-saved since, and that is the working tree whatever mode differ
// was started in.
func TestPersist_CommentsSurviveAModeChange(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")
	tr.Stage("src.ts")

	// Written in plain mode.
	m := liveModelMode(t, tr, false)
	m = settle(t, m, key("r"))
	updated, _ := m.startComment()
	m = updated.(Model)
	m.commentInput.SetValue("this needs a second look")
	updated, _ = m.saveComment()
	m = updated.(Model)
	if m.session.CountFor("src.ts") != 1 {
		t.Fatalf("the comment was not saved (%d)", m.session.CountFor("src.ts"))
	}

	// Reopened with -s, the file untouched in between.
	staged := liveModelMode(t, tr, true)
	if staged.session == nil {
		t.Fatal("no session was restored at all")
	}
	if got := staged.session.CountFor("src.ts"); got != 1 {
		t.Errorf("%d comments survived reopening with -s, want 1", got)
	}
}

// And the other direction, which is the one the write side gets wrong.
//
// Under -s the keys the refresh measured are index object ids. Writing those
// as the stored key means a later plain run compares a worktree hash against
// an index oid, never matches, and drops the comment — so persistence has to
// measure the working tree even when change detection is not.
func TestPersist_CommentsWrittenUnderStagedModeSurvivePlainMode(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")
	tr.Stage("src.ts")

	m := liveModelMode(t, tr, true)
	if len(m.files) == 0 {
		t.Skip("nothing staged in this fixture")
	}
	m = settle(t, m, key("r"))
	updated, _ := m.startComment()
	m = updated.(Model)
	m.commentInput.SetValue("written while reviewing the index")
	updated, _ = m.saveComment()
	m = updated.(Model)
	path := m.currentFilePath()
	if m.session.CountFor(path) != 1 {
		t.Fatalf("the comment was not saved (%d)", m.session.CountFor(path))
	}

	plain := liveModelMode(t, tr, false)
	if plain.session == nil {
		t.Fatal("no session was restored at all")
	}
	if got := plain.session.CountFor(path); got != 1 {
		t.Errorf("%d comments survived reopening without -s, want 1", got)
	}
}

// And the rule still holds across the mode change: a file the agent re-saved
// loses its comments.
func TestPersist_AReSavedFileLosesItsCommentsInEitherMode(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := liveModelMode(t, tr, false)
	m = settle(t, m, key("r"))
	updated, _ := m.startComment()
	m = updated.(Model)
	m.commentInput.SetValue("about the old content")
	updated, _ = m.saveComment()
	m = updated.(Model)

	// The agent rewrites it while differ is closed.
	tr.Modify("src.ts", "one\nREWRITTEN BY THE AGENT\n")

	for _, stagedOnly := range []bool{false, true} {
		reopened := liveModelMode(t, tr, stagedOnly)
		// No `continue` on a nil session: that skip made both halves of this
		// loop vacuous, because everything was dropped in both.
		if got := countFor(reopened.session, "src.ts"); got != 0 {
			t.Errorf("stagedOnly=%v: %d comments survived the file being rewritten",
				stagedOnly, got)
		}
	}
}

// And the other half of that claim, which the test above cannot make: a
// comment does come back when the file has *not* been rewritten, in either
// mode. Without it "dropped in both modes" is also satisfied by a store that
// never restores anything.
func TestPersist_AnUntouchedFileKeepsItsCommentsInEitherMode(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")

	m := liveModelMode(t, tr, false)
	m = settle(t, m, key("r"))
	updated, _ := m.startComment()
	m = updated.(Model)
	m.commentInput.SetValue("about the current content")
	updated, _ = m.saveComment()
	m = updated.(Model)

	reopened := liveModelMode(t, tr, false)
	if got := countFor(reopened.session, "src.ts"); got != 1 {
		t.Errorf("%d comments came back from an untouched file, want 1", got)
	}
}

func liveModelMode(t *testing.T, tr *testutil.Repo, stagedOnly bool) Model {
	t.Helper()
	repo, err := git.NewRepo(tr.Dir)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := repo.ChangedFiles(stagedOnly, "")
	if err != nil {
		t.Fatal(err)
	}
	var untracked []string
	if !stagedOnly {
		if untracked, err = repo.UntrackedFiles(); err != nil {
			t.Fatal(err)
		}
	}
	th := theme.Themes["dark"]
	m := NewModel(repo, config.Default(), changes, untracked, NewStyles(th), th, stagedOnly, "")
	m = settle(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	// A refresh, as the first tick does — it is what fills m.fileKeys, and
	// under -s those are index oids. Without it nothing here exercises the
	// question of which key persistence may reuse.
	m = settle(t, m, m.refreshFilesCmd()())
	if len(m.files) > 0 && len(m.fileKeys) == 0 {
		t.Fatal("the refresh recorded no file keys")
	}
	if cmd := m.loadDiffCmd(true); cmd != nil {
		m = settle(t, m, cmd())
	}
	return m
}

// The fingerprint has to be of the content the reviewer read, not of
// whatever the file held when the review was last written out.
//
// The key used to be measured at save time, and preferred m.fileKeys — which
// the two-second poll refills from disk. So: comment on v1, the agent writes
// v2, the poll notices, the reviewer comments on another file, and the save
// recorded v2's key for the first comment. It then came back on the next
// start presented as valid against a version its author never saw, which is
// the one thing the fingerprint exists to prevent.
func TestPersist_ACommentIsKeyedToTheContentItsAuthorRead(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "first")
	tr.CommitFile("b.ts", "alpha\n", "second")
	tr.Modify("a.ts", "one\nV1\n")
	tr.Modify("b.ts", "BETA\n")

	m := settle(t, liveModel(t, tr), key("r"))
	m = writeComment(t, m, "V1", "about v1")

	// The agent rewrites a.ts, and differ's poll notices: this is what put
	// the new content's key on the old comment.
	tr.ExternalEdit("a.ts", "one\nV2\n")
	m = settle(t, m, tickMsg(time.Now()))

	// Something else is saved, which used to rewrite a.ts's key as a side
	// effect.
	m = settle(t, m, key("n"))               // next file
	writeComment(t, m, "BETA", "about beta") // saved to disk; the model is done with

	reopened := restart(t, tr)
	if got := countFor(reopened.session, "a.ts"); got != 0 {
		t.Errorf("%d comments about a.ts came back, and the file is no longer "+
			"the version they were written about", got)
	}
}

// Under -s the reviewer reads the index, and an unstaged edit does not touch
// what they read. Fingerprinting the working tree in every mode dropped
// comments about staged content that had not moved.
func TestPersist_UnderStagedOnlyAnUnstagedEditKeepsTheComments(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nSTAGED\n")
	tr.Stage("src.ts")

	m := settle(t, liveModelMode(t, tr, true), key("r"))
	writeComment(t, m, "STAGED", "about the staged content")

	// The working tree moves; the index does not.
	tr.ExternalEdit("src.ts", "one\nSTAGED\nand an unstaged line\n")

	reopened := liveModelMode(t, tr, true)
	if got := countFor(reopened.session, "src.ts"); got != 1 {
		t.Errorf("%d comments came back, want 1 — the staged content the "+
			"comment is about has not moved", got)
	}
}

// And the other direction: staging a change the comment was written about
// under -s does move the index, so the comment goes.
func TestPersist_UnderStagedOnlyStagingSomethingElseDropsThem(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nSTAGED\n")
	tr.Stage("src.ts")

	m := settle(t, liveModelMode(t, tr, true), key("r"))
	writeComment(t, m, "STAGED", "about the staged content")

	tr.ExternalEdit("src.ts", "one\nRESTAGED\n")
	tr.Stage("src.ts")

	reopened := liveModelMode(t, tr, true)
	if got := countFor(reopened.session, "src.ts"); got != 0 {
		t.Errorf("%d comments survived the staged content being replaced", got)
	}
}

// In default mode the cursor can sit on a staged entry: git reports a file
// with both staged and unstaged changes twice, staged first, and that entry's
// diff is `--cached`. So the scope is decided by the entry the diff came
// from, not by the flag differ was started with — keying on the flag called
// the index the working tree, and an unstaged edit then dropped a comment
// about staged content that had not moved.
func TestPersist_TheScopeComesFromTheEntryNotTheFlag(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nSTAGED\n")
	tr.Stage("src.ts")
	tr.ExternalEdit("src.ts", "one\nSTAGED\nunstaged tail\n")

	m := settle(t, liveModel(t, tr), key("r"))
	if !m.files[m.cursor].change.Staged {
		t.Fatalf("the fixture does not put the cursor on the staged entry: %+v",
			m.files[m.cursor].change)
	}
	if m.stagedOnly {
		t.Fatal("this test is about default mode")
	}
	m = writeComment(t, m, "STAGED", "about the staged content")

	// Another unstaged edit. The index still holds exactly what the comment
	// was written about.
	tr.ExternalEdit("src.ts", "one\nSTAGED\nunstaged tail\nand more\n")

	reopened := restart(t, tr)
	if got := countFor(reopened.session, "src.ts"); got != 1 {
		t.Errorf("%d comments came back, want 1 — the staged content the "+
			"comment was written about has not moved", got)
	}
}

// Two differs in one repository. One per tmux pane is the ordinary way to
// use differ, and each save serialises the whole session over review.json
// with no merge — so the second silently destroyed the first's comments, and
// brought back a comment the first had already delivered, which would send
// the agent a review it has already acted on.
//
// The second instance now reviews without saving and says so.
func TestPersist_ASecondDifferDoesNotDestroyTheFirstsReview(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.CommitFile("b.ts", "alpha\n", "second")
	tr.Modify("a.ts", "AAA\n")
	tr.Modify("b.ts", "BBB\n")

	// The first differ takes the lock and keeps it, as a running process
	// would. A live foreign pid stands in for it: pid 1 is always running.
	gitDir := filepath.Join(tr.Dir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "differ"), 0o755); err != nil {
		t.Fatal(err)
	}
	first := settle(t, liveModel(t, tr), key("r"))
	writeComment(t, first, "AAA", "from the first differ")
	writeForeignLock(t, gitDir)

	// A second differ starts while that one is running.
	fresh := liveModel(t, tr)
	if !strings.Contains(fresh.statusMsg, "another differ") {
		t.Errorf("the second differ starts saying %q, which does not mention the first", fresh.statusMsg)
	}
	second := settle(t, fresh, key("r"))
	if second.store != nil {
		t.Error("the second differ took the review file")
	}
	second = moveTo(t, second, "b.ts")
	second = writeComment(t, second, "BBB", "from the second differ")

	// The first differ's comment is still the one on disk.
	removeForeignLock(t, gitDir)
	back := restart(t, tr)
	if got := countFor(back.session, "a.ts"); got != 1 {
		t.Errorf("%d comments from the first differ survived, want 1", got)
	}
	if got := countFor(back.session, "b.ts"); got != 0 {
		t.Errorf("the second differ wrote %d comments it said it would not save", got)
	}
}

// moveTo puts the cursor on a named file.
func moveTo(t *testing.T, m Model, path string) Model {
	t.Helper()
	for range len(m.files) {
		if m.currentFilePath() == path {
			return m
		}
		m = settle(t, m, key("n"))
	}
	t.Fatalf("%s is not in the changeset", path)
	return m
}

func writeForeignLock(t *testing.T, gitDir string) {
	t.Helper()
	// pid 1: always running, never this process.
	if err := os.WriteFile(filepath.Join(gitDir, "differ", "review.lock"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func removeForeignLock(t *testing.T, gitDir string) {
	t.Helper()
	if err := os.Remove(filepath.Join(gitDir, "differ", "review.lock")); err != nil {
		t.Fatal(err)
	}
}

// A send that failed is the one most worth being able to look up again, so
// the attempt is written out like any other change to the review. Removing
// that one call left the claim untested.
func TestPersist_AFailedSendIsWrittenOut(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "AAA\n")

	m := settle(t, liveModel(t, tr), key("r"))
	m = writeComment(t, m, "AAA", "please fix")
	ids := []string{m.session.CommentsFor("a.ts")[0].ID}

	updated, _ := m.Update(feedbackSentMsg{
		ids: ids, target: "tmux",
		err: errors.New("tmux target \"%99\" does not match a pane"),
	})
	m = updated.(Model)

	back := restart(t, tr)
	if back.session == nil {
		t.Fatal("nothing was restored")
	}
	var found bool
	for _, d := range back.session.History() {
		if d.Err != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("the failed attempt was not written out: %+v", back.session.History())
	}
	// And the comment is still pending, because it never arrived.
	if got := countFor(back.session, "a.ts"); got != 1 {
		t.Errorf("%d comments came back, want 1 — a failed send costs nothing", got)
	}
}

// An hour of reading that produced no comment is still an hour of reading.
// Only comments were saved, so the progress in the bar went backwards across
// a restart for every file read and not commented on.
func TestPersist_AFileReadWithoutCommentComesBackRead(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.CommitFile("b.ts", "alpha\n", "second")
	tr.Modify("a.ts", "AAA\n")
	tr.Modify("b.ts", "BBB\n")

	m := settle(t, liveModel(t, tr), key("r"))
	// b.ts is commented on, which saves the review; a.ts is only read.
	m = moveTo(t, m, "b.ts")
	writeComment(t, m, "BBB", "a note")

	back := restart(t, tr)
	if got := stateOf(back.session, "a.ts"); got == review.FileUnreviewed {
		t.Error("a file that was read came back unreviewed")
	}
}

// And a file that was read and then rewritten does not: reading it told you
// about content that is no longer there.
func TestPersist_AFileReadThenRewrittenComesBackUnread(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.CommitFile("b.ts", "alpha\n", "second")
	tr.Modify("a.ts", "AAA\n")
	tr.Modify("b.ts", "BBB\n")

	m := settle(t, liveModel(t, tr), key("r"))
	m = moveTo(t, m, "b.ts")
	writeComment(t, m, "BBB", "a note")

	tr.ExternalEdit("a.ts", "REWRITTEN BY THE AGENT\n")

	back := restart(t, tr)
	if got := stateOf(back.session, "a.ts"); got != review.FileUnreviewed {
		t.Errorf("a.ts came back %v after being rewritten, want unreviewed", got)
	}
}
