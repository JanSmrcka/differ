package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
)

// Failures used to be git's stderr concatenated into the status bar:
//
//	commit failed: fatal: Unable to create '/repo/.git/index.lock': File exists.
//	If no other git process is currently running, this probably means a git
//	process crashed in this repository earlier. Make sure no other git process
//	is running and remove the file manually to continue.
//
// which is three lines of somebody else's voice in a one-line bar, and says
// nothing about what to do.

func TestProblem_AGitFailureIsConciseAndSaysWhatToDo(t *testing.T) {
	t.Parallel()
	lock := errors.New("fatal: Unable to create '/repo/.git/index.lock': File exists.\n\nIf no other git process is currently running, this probably means a\ngit process crashed in this repository earlier.")

	p := describe("commit", lock)

	if !strings.Contains(p.summary, "commit") {
		t.Errorf("summary %q does not say what failed", p.summary)
	}
	if p.hint == "" {
		t.Error("no hint about what to do")
	}
	if strings.Contains(p.line(), "\n") {
		t.Errorf("the status line is multi-line:\n%s", p.line())
	}
	if strings.Contains(p.line(), "index.lock") {
		t.Errorf("git's own words leaked into the status line: %q", p.line())
	}
	// But they are kept, because sometimes they are the only useful thing.
	if !strings.Contains(p.detail, "index.lock") {
		t.Errorf("the original text was thrown away: %q", p.detail)
	}
}

func TestProblem_TheHintFitsTheFailure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, action, err string
		wantHint          string
	}{
		{
			name: "a held index", action: "stage",
			err:      "fatal: Unable to create '/r/.git/index.lock': File exists.",
			wantHint: "another git process",
		},
		{
			name: "no such ref", action: "compare",
			err:      "fatal: ambiguous argument 'nope': unknown revision or path not in the working tree.",
			wantHint: "no branch, tag or commit",
		},
		{
			name: "no upstream", action: "push",
			err:      "fatal: The current branch topic has no upstream branch.",
			wantHint: "upstream",
		},
		{
			name: "a diverged branch", action: "pull",
			err:      "fatal: Not possible to fast-forward, aborting.",
			wantHint: "diverged",
		},
		{
			name: "local changes in the way", action: "switch",
			err:      "error: Your local changes to the following files would be overwritten by checkout:",
			wantHint: "commit or stash",
		},
		{
			name: "the remote refused", action: "push",
			err:      "fatal: Could not read from remote repository.\nPlease make sure you have the correct access rights",
			wantHint: "access to the remote",
		},
		{
			name: "a branch that already exists", action: "create branch",
			err:      "fatal: a branch named 'topic' already exists",
			wantHint: "already",
		},
		{
			name: "a tmux target that is gone", action: "send",
			err:      "tmux send-keys: exit status 1: can't find pane: %9",
			wantHint: "tmux",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := describe(c.action, errors.New(c.err))
			if !strings.Contains(p.hint, c.wantHint) {
				t.Errorf("hint for %q is %q, want it to mention %q", c.err, p.hint, c.wantHint)
			}
		})
	}
}

// An unrecognised failure still has to be presented, not dumped: one line of
// the tool's own words, and the rest kept for the details view.
func TestProblem_AnUnknownFailureIsStillOneLine(t *testing.T) {
	t.Parallel()
	p := describe("commit", errors.New("something nobody anticipated\nacross two lines"))

	if strings.Contains(p.line(), "\n") {
		t.Errorf("the status line is multi-line: %q", p.line())
	}
	if !strings.Contains(p.detail, "two lines") {
		t.Error("the full text was not kept")
	}
}

// The model's failure path: a concise line in the bar, the detail available,
// and nothing else disturbed.
func TestProblem_AFailureLeavesTheReviewAlone(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}}})
	m.mode = modeDiff
	m.cursor, m.diffCursor = 0, 7

	updated, _ := m.handleCommitDone(commitDoneMsg{err: errors.New("fatal: Unable to create '/r/.git/index.lock': File exists.")})
	got := updated.(Model)

	if !strings.Contains(got.statusMsg, "another git process") {
		t.Errorf("status bar = %q, want the hint", got.statusMsg)
	}
	if strings.Contains(got.statusMsg, "index.lock") {
		t.Errorf("raw stderr in the status bar: %q", got.statusMsg)
	}
	if got.diffCursor != 7 {
		t.Errorf("the cursor moved to %d — a failure must not disturb the review", got.diffCursor)
	}
	if got.problem == nil || !strings.Contains(got.problem.detail, "index.lock") {
		t.Error("the detail was not kept for the details view")
	}
}

// ! shows what the tool actually said.
//
// It is global in the same sense the command bar is: everywhere a key is a
// command rather than a character. Where the user is typing — a commit
// message, a branch name, a comment — it is text, exactly as ? is.
// TestKeymap_TheGlobalKeysWorkInEveryMode holds it to that in all nine
// states.
func TestProblem_BangShowsTheDetail(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}}})
	updated, _ := m.handlePushDone(pushDoneMsg{err: errors.New("fatal: Could not read from remote repository.\nPlease make sure you have the correct access rights.")})
	m = updated.(Model)

	opened, _ := m.routeKey(key("!"))
	m = opened.(Model)
	if !m.showProblem {
		t.Fatal("! did not open the details")
	}

	got := stripANSI(m.renderProblemOverlay(80, 16))
	for _, want := range []string{"push failed", "access to the remote", "correct access rights"} {
		if !strings.Contains(got, want) {
			t.Errorf("the details do not mention %q:\n%s", want, got)
		}
	}

	closed, _ := m.routeKey(tea.KeyMsg{Type: tea.KeyEsc})
	if closed.(Model).showProblem {
		t.Error("esc did not close the details")
	}
}

// Nothing having gone wrong is a normal state, not a blank panel.
func TestProblem_TheDetailsSayWhenThereIsNothingToShow(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	if got := stripANSI(m.renderProblemOverlay(80, 10)); !strings.Contains(got, "nothing has gone wrong") {
		t.Errorf("an empty problem view renders %q", got)
	}
}

// An empty changeset used to be an empty panel, which reads as a bug.
func TestEmptyState_SaysWhatIsTrueAndWhatToDo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(m Model) Model
		wants []string
	}{
		{
			name:  "a clean working tree",
			setup: func(m Model) Model { return m },
			wants: []string{"No changes", "clean"},
		},
		{
			name:  "nothing staged under -s",
			setup: func(m Model) Model { m.stagedOnly = true; return m },
			wants: []string{"Nothing staged", "drop -s"},
		},
		{
			name:  "nothing differs from a ref",
			setup: func(m Model) Model { m.ref = "main"; return m },
			wants: []string{"No differences", "main"},
		},
		{
			name:  "nothing to review",
			setup: func(m Model) Model { m.mode = modeReview; return m },
			wants: []string{"Nothing to review"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m := c.setup(newTestModel(t, nil))
			got := stripANSI(m.renderFileList())
			for _, want := range c.wants {
				if !strings.Contains(got, want) {
					t.Errorf("the empty panel does not mention %q:\n%s", want, got)
				}
			}
		})
	}
}

// And a changeset that is not empty must not get an empty state.
func TestEmptyState_NotShownWhenThereAreFiles(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}}})
	if got := stripANSI(m.renderFileList()); strings.Contains(got, "No changes") {
		t.Errorf("an empty state was drawn over a real changeset:\n%s", got)
	}
}

// Every case above hands describe an error built by hand, which is how the
// whole hint table came to be dead in production without a single test
// failing: git's words never reached it. `cmd.Output()` returns an
// *exec.ExitError whose Error() is only "exit status 1".
//
// This drives real failing git commands through Repo → fail → the status bar.
func TestProblem_RealGitFailuresReachTheHintTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		fail     func(t *testing.T, tr *testutil.Repo, repo *git.Repo) error
		action   string
		wantHint string
	}{
		{
			name:   "committing with nothing staged",
			action: "commit",
			fail: func(t *testing.T, tr *testutil.Repo, repo *git.Repo) error {
				return repo.Commit("nothing here")
			},
			wantHint: "nothing is staged",
		},
		{
			name:   "a branch that already exists",
			action: "creating the branch",
			fail: func(t *testing.T, tr *testutil.Repo, repo *git.Repo) error {
				return repo.CreateBranch("master")
			},
			wantHint: "already exists",
		},
		{
			name:   "a ref that does not exist",
			action: "refresh",
			fail: func(t *testing.T, tr *testutil.Repo, repo *git.Repo) error {
				_, err := repo.ChangedFiles(false, "no-such-ref")
				return err
			},
			wantHint: "no branch, tag or commit",
		},
		{
			name:   "an index held by another git",
			action: "commit",
			fail: func(t *testing.T, tr *testutil.Repo, repo *git.Repo) error {
				tr.Write("dummy.txt", "x")
				tr.Stage("dummy.txt")
				if err := os.WriteFile(filepath.Join(tr.Dir, ".git", "index.lock"), []byte(""), 0o644); err != nil {
					t.Fatal(err)
				}
				return repo.Commit("blocked")
			},
			wantHint: "another git process",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			tr := testutil.NewRepo(t)
			tr.CommitFile("a.ts", "one\n", "init")
			repo, err := git.NewRepo(tr.Dir)
			if err != nil {
				t.Fatal(err)
			}

			gitErr := c.fail(t, tr, repo)
			if gitErr == nil {
				t.Fatal("the git command was expected to fail and did not")
			}

			m := newTestModel(t, nil).fail(c.action, gitErr)
			if !strings.Contains(m.statusMsg, c.wantHint) {
				t.Errorf("status bar = %q, want it to mention %q\n(git said: %q)",
					m.statusMsg, c.wantHint, gitErr)
			}
			if m.problem == nil || strings.TrimSpace(m.problem.detail) == "" {
				t.Error("no detail kept, so ! would show nothing")
			}
			if strings.HasPrefix(m.problem.detail, "exit status") {
				t.Errorf("the detail is just an exit code: %q", m.problem.detail)
			}
		})
	}
}

// The panel is 35 columns and padTo only pads, so a long explanation ran past
// it and kinked the divider between the two halves of the layout. A ref name
// can be any length.
func TestEmptyState_FitsThePanelWhateverItSays(t *testing.T) {
	t.Parallel()
	for _, setup := range []func(m Model) Model{
		func(m Model) Model { return m },
		func(m Model) Model { m.stagedOnly = true; return m },
		func(m Model) Model { m.ref = "origin/feat/errors-and-empty-states-55"; return m },
		func(m Model) Model { m.mode = modeReview; return m },
	} {
		// The smallest terminal differ draws, with a status message — which
		// costs the panel a row and is where the breathing room around the
		// text has to give way to the text itself.
		for _, size := range []struct {
			w, h   int
			status string
		}{
			{120, 40, ""},
			{80, 24, ""},
			{60, 10, ""},
			{60, 10, "nothing to review"},
		} {
			m := setup(newTestModel(t, nil))
			m.width, m.height, m.statusMsg = size.w, size.h, size.status

			rows := strings.Split(m.renderFileList(), "\n")
			for i, row := range rows {
				if got := lipgloss.Width(row); got != fileListWidth {
					t.Errorf("%dx%d: row %d is %d columns, want %d: %q",
						size.w, size.h, i, got, fileListWidth, stripANSI(row))
				}
			}
			if len(rows) > m.listHeight() {
				t.Errorf("%dx%d status=%q: the empty state is %d rows in a %d-row panel",
					size.w, size.h, size.status, len(rows), m.listHeight())
			}
			// And the explanation is what has to survive, not the padding.
			if !strings.Contains(stripANSI(m.renderFileList()), strings.Fields(m.emptyState()[1])[0]) {
				t.Errorf("%dx%d status=%q: the explanation was dropped:\n%s",
					size.w, size.h, size.status, stripANSI(m.renderFileList()))
			}
		}
	}
}

// Entering review mode with nothing to review still has to leave a working
// review: a changeset arrives a moment later, and a review with no session has
// no progress, no badges, and no way out but to leave and come back.
func TestProblem_AnEmptyReviewIsStillAWorkingReview(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, nil)
	updated, _ := m.enterReviewMode()
	m = updated.(Model)

	if m.mode != modeReview {
		t.Fatalf("mode = %v, want review", m.mode)
	}
	if m.session == nil {
		t.Fatal("review mode opened without a session")
	}

	// The changeset arrives.
	arrived, _ := m.handleFilesRefreshed(filesRefreshedMsg{
		files: []fileItem{
			{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}},
			{change: git.FileChange{Path: "b.ts", Status: git.StatusModified}},
		},
		keys: map[string]string{"a.ts": "k", "b.ts": "k"},
	})
	m = arrived.(Model)

	moved, _ := m.updateReviewMode(key("n"))
	m = moved.(Model)
	if got := m.reviewSummary(); !strings.Contains(got, "/2") {
		t.Errorf("progress = %q, want it to count the two files that arrived", got)
	}
}

// The affordance has to survive the one-row bar, which drops whole words with
// no ellipsis — so it goes before the hint, and the failure goes before the
// review chatter that shares the row.
func TestProblem_TheAffordanceSurvivesANarrowBar(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}}})
	m.mode = modeReview
	m.session = review.NewSession()
	m.splitDiff = true
	m = m.fail("generating a commit message", errors.New("fatal: Unable to create '/r/.git/index.lock': File exists."))

	for _, width := range []int{60, 72, 80, 120} {
		m.width = width
		got := stripANSI(m.renderHintBar())
		if !strings.Contains(got, "!") {
			t.Errorf("width %d: the bar does not offer !:\n%s", width, got)
		}
		if !strings.Contains(got, "failed") {
			t.Errorf("width %d: the bar does not say anything failed:\n%s", width, got)
		}
	}
}

// Fragments are matched against a message that quotes names the *user* chose,
// so a file called "Permission to Travel.md" must not be answered with advice
// about remote credentials. Every case here is a real git message shape.
func TestProblem_AFragmentDoesNotMatchAUserChosenName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, err, wantHint string
	}{
		{
			name:     "a path containing the words permission to",
			err:      "error: unable to unlink old 'docs/Permission to build.pdf': Permission denied",
			wantHint: "permissions on that path",
		},
		{
			name:     "a pathspec naming a conflict",
			err:      "fatal: pathspec 'src/conflict (old).ts' did not match any file(s) known to git",
			wantHint: "no file, branch or ref",
		},
		{
			name:     "a pathspec naming local changes",
			err:      "fatal: pathspec 'docs/local changes.md' did not match any file(s) known to git",
			wantHint: "no file, branch or ref",
		},
		{
			name:     "a pathspec naming tracking information",
			err:      "error: pathspec 'no tracking information.txt' did not match any file(s) known to git",
			wantHint: "no file, branch or ref",
		},
		{
			name:     "a branch named after a conflict",
			err:      "fatal: a branch named 'fix/conflict-handling' already exists",
			wantHint: "already exists",
		},
		// And a hook or a protected branch refusing a push is not something
		// pulling can fix, which is what the generic push fragment advised.
		{
			name: "a pre-receive hook declining",
			err: "remote: policy: signed commits only\nTo /tmp/remote.git\n" +
				" ! [remote rejected] master -> master (pre-receive hook declined)\n" +
				"error: failed to push some refs to '/tmp/remote.git'",
			wantHint: "refused the push",
		},
		{
			name: "a genuinely behind push",
			err: "To /tmp/remote.git\n ! [rejected]        master -> master (fetch first)\n" +
				"error: failed to push some refs to '/tmp/remote.git'",
			wantHint: "pull with F first",
		},
		{
			name:     "GitHub refusing over HTTPS",
			err:      "remote: Permission to foo/bar.git denied to someone.\nfatal: unable to access 'https://github.com/foo/bar.git/'",
			wantHint: "access to the remote",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := describe("push", errors.New(c.err)); !strings.Contains(got.hint, c.wantHint) {
				t.Errorf("hint = %q, want it to mention %q", got.hint, c.wantHint)
			}
		})
	}
}

// git push opens with "To <url>", which is never the sentence that matters —
// but a sentence that merely starts with "To" is.
func TestProblem_OnlyPushsBannerIsSkipped(t *testing.T) {
	t.Parallel()
	if got := describe("commit", errors.New("To commit, stage something first.\nhint: use tab")); !strings.Contains(got.hint, "To commit") {
		t.Errorf("hint = %q, want the sentence kept", got.hint)
	}
	if got := describe("push", errors.New("To /tmp/x\nsomething useful")); !strings.Contains(got.hint, "something useful") {
		t.Errorf("hint = %q, want the banner skipped", got.hint)
	}
	// Nothing but banners: better the banner than nothing.
	if got := describe("push", errors.New("To /tmp/a\nTo /tmp/b")); got.hint == "" {
		t.Error("a message of nothing but banners produced no hint at all")
	}
}

// A hint is capped, because git's first line can be a paragraph and the bar is
// one row.
func TestProblem_ALongFallbackHintIsCapped(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a very long explanation ", 20)
	got := describe("commit", errors.New(long))

	if w := lipgloss.Width(got.hint); w > maxHintWidth {
		t.Errorf("hint is %d columns, want at most %d", w, maxHintWidth)
	}
	if !strings.HasSuffix(got.hint, "…") {
		t.Errorf("the hint was cut without saying so: %q", got.hint)
	}
}

// The affordance is before the hint, and a test says so — moving it back was
// otherwise free.
func TestProblem_TheAffordanceComesBeforeTheHint(t *testing.T) {
	t.Parallel()
	line := describe("push", errors.New("fatal: Could not read from remote repository.")).line()
	bang, hint := strings.Index(line, "!"), strings.Index(line, "access to the remote")
	if bang < 0 || hint < 0 {
		t.Fatalf("line is missing the affordance or the hint: %q", line)
	}
	if bang > hint {
		t.Errorf("the affordance comes after the hint, so a long hint cuts it off: %q", line)
	}
}
