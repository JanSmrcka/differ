package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jansmrcka/differ/internal/git"
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
			wants: []string{"Nothing staged", "-s"},
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
