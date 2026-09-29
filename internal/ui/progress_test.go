package ui

import (
	"testing"

	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/testutil"
)

// The fingerprint that decides whether a file moved under the reviewer.
//
// It was written against hand-made maps first, which meant the fingerprint
// itself had no test at all: replacing it with a constant left the suite
// green. These drive it against a real repository, because the ways it gets
// this wrong are all git's doing — staging shuffles line counts between the
// staged and unstaged halves of the same path, and `-s` reviews the index
// while the fingerprint was reading the worktree.

// keysOn builds the fingerprints the way a refresh does.
func keysOn(t *testing.T, tr *testutil.Repo, stagedOnly bool) map[string]string {
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
	return fileKeysOf(repo, buildFileItems(repo, changes, untracked), stagedOnly)
}

func TestFileKeys_AnEditMovesTheKey(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "init")
	tr.Modify("a.ts", "one\nTWO\n")

	before := keysOn(t, tr, false)
	tr.Modify("a.ts", "one\nTHREE\n")
	after := keysOn(t, tr, false)

	if before["a.ts"] == "" || after["a.ts"] == "" {
		t.Fatalf("no key for a.ts: %v then %v", before, after)
	}
	if before["a.ts"] == after["a.ts"] {
		t.Errorf("an edit did not move the key: %q", before["a.ts"])
	}
}

// Staging changes nothing about the content, and the user pressing `a` must
// not make every file they have read look rewritten.
func TestFileKeys_StagingAloneDoesNotMoveTheKey(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "init")
	tr.Modify("a.ts", "one\nTWO\n")

	before := keysOn(t, tr, false)
	tr.Stage("a.ts")
	after := keysOn(t, tr, false)

	if before["a.ts"] != after["a.ts"] {
		t.Errorf("git add moved the key:\n%q\n%q", before["a.ts"], after["a.ts"])
	}
}

// Under -s the reviewer is looking at the index. A worktree write they have
// not staged is not a change to what is on screen.
func TestFileKeys_StagedOnlyIgnoresAWorktreeEdit(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "init")
	tr.Modify("a.ts", "one\nTWO\n")
	tr.Stage("a.ts")

	before := keysOn(t, tr, true)
	tr.Write("a.ts", "one\nTWO\nunstaged\n")
	after := keysOn(t, tr, true)

	if before["a.ts"] == "" {
		t.Fatalf("no staged key for a.ts: %v", before)
	}
	if before["a.ts"] != after["a.ts"] {
		t.Errorf("an unstaged write moved the staged key:\n%q\n%q", before["a.ts"], after["a.ts"])
	}
}

// A file with both staged and unstaged changes appears twice in the file list.
// One fingerprint per path has to cover both halves, or the invisible half can
// change without anyone noticing.
func TestFileKeys_OnePathWithBothHalvesGetsOneStableKey(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "init")
	tr.Modify("a.ts", "one\nTWO\n")
	tr.Stage("a.ts")
	tr.Write("a.ts", "one\nTWO\nthree\n")

	keys := keysOn(t, tr, false)
	if len(keys) != 1 {
		t.Fatalf("got %d keys for one path: %v", len(keys), keys)
	}

	before := keys["a.ts"]
	tr.Stage("a.ts")
	if after := keysOn(t, tr, false)["a.ts"]; before != after {
		t.Errorf("staging the second half moved the key:\n%q\n%q", before, after)
	}
}

// The other half of -s: a staged edit is a change to what is on screen, and
// the line counts are the only signal left once the worktree stat is skipped.
func TestFileKeys_StagedOnlyNoticesAStagedEdit(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\ntwo\n", "init")
	tr.Modify("a.ts", "one\nTWO\n")
	tr.Stage("a.ts")

	before := keysOn(t, tr, true)
	tr.Write("a.ts", "one\nTWO\nthree\n")
	tr.Stage("a.ts")
	after := keysOn(t, tr, true)

	if before["a.ts"] == after["a.ts"] {
		t.Errorf("a staged edit did not move the staged key: %q", before["a.ts"])
	}
}

// The ordinary agent loop: it commits a slice of its work, so the file leaves
// the changeset, then edits it again. Requiring a previous fingerprint meant
// this case was never flagged.
func TestProgress_AFileThatLeavesAndComesBackIsMarkedChanged(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	m.files = []fileItem{
		{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}},
		{change: git.FileChange{Path: "b.ts", Status: git.StatusModified}},
	}
	m.fileKeys = map[string]string{"a.ts": "k1", "b.ts": "k1"}
	m.session.MarkViewed("a.ts")
	m.session.MarkViewed("b.ts")

	// b.ts gets committed: gone from the changeset.
	gone, _ := m.handleFilesRefreshed(filesRefreshedMsg{
		files: m.files[:1],
		keys:  map[string]string{"a.ts": "k1"},
	})
	m = gone.(Model)

	// …and then rewritten, so it is back with different content.
	back, _ := m.handleFilesRefreshed(filesRefreshedMsg{
		files: m.files,
		keys:  map[string]string{"a.ts": "k1", "b.ts": "k2"},
	})
	m = back.(Model)

	if !m.session.ChangedSinceViewed("b.ts") {
		t.Error("b.ts left the changeset and came back rewritten, and was not marked")
	}
}

// A file that was never read cannot go stale on its reader, so appearing for
// the first time must not put a badge on it.
func TestProgress_ANewFileIsNotMarkedChanged(t *testing.T) {
	t.Parallel()
	m := historyModel(t)
	m.files = []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}}}
	m.fileKeys = map[string]string{"a.ts": "k1"}

	updated, _ := m.handleFilesRefreshed(filesRefreshedMsg{
		files: m.files,
		keys:  map[string]string{"a.ts": "k1", "new.ts": "k9"},
	})
	if updated.(Model).session.ChangedSinceViewed("new.ts") {
		t.Error("a file that just appeared was marked changed")
	}
}
