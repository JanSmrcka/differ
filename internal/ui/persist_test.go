package ui

import (
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

	if second.session != nil {
		if n := second.session.CountFor("src.ts"); n != 0 {
			t.Errorf("src.ts came back with %d comments, want none", n)
		}
		if got := second.session.FileStateOf("src.ts"); got != review.FileUnreviewed {
			t.Errorf("src.ts state = %v, want unreviewed", got)
		}
	}
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
