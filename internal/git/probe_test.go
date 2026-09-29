package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jansmrcka/differ/internal/testutil"
)

// The probe is what the poll loop asks "did anything move?" — so two calls with
// nothing happening in between must agree, or every tick would rebuild the file
// list and reload the diff.
func TestProbe_IsStableWhenNothingChanges(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	writeFile(t, repo, "a.txt", "one\n")

	first, err := repo.Probe()
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	second, err := repo.Probe()
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if first != second {
		t.Errorf("probe moved with no change:\n  %q\n  %q", first, second)
	}
	if first == "" {
		t.Error("probe is empty, so it could never detect anything")
	}
}

// The case the whole design turns on. git status reports a modified file as
// "1 .M <oids> path", and those oids are HEAD's and the index's — not the
// working tree's. So an agent editing a file it has already modified produces
// byte-identical status output, and a fingerprint taken from status alone would
// say nothing happened. That is the commonest thing that happens during a
// review.
func TestProbe_MovesWhenAnAlreadyModifiedFileIsEditedAgain(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	testutil.GitIn(t, repo.Dir(), "add", "a.txt")
	testutil.GitIn(t, repo.Dir(), "commit", "-m", "add a")

	writeFile(t, repo, "a.txt", "one\ntwo\n")
	first, err := repo.Probe()
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	writeFile(t, repo, "a.txt", "one\ntwo\nthree\n")
	second, err := repo.Probe()
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if first == second {
		t.Errorf("a second edit of a modified file left the probe unchanged: %q", first)
	}
}

// probedPaths reads real porcelain-v2 -z records. The shapes below were taken
// from git itself, not written from the manual: a rename emits the new path in
// its own record and the original path as the next one, and -z leaves spaces in
// paths alone instead of quoting them.
func TestProbedPaths_ReadsGitsOwnRecords(t *testing.T) {
	t.Parallel()
	status := strings.Join([]string{
		"# branch.oid e72e4cec2543ecab39a0167f4014f005f6c5f5ef",
		"# branch.head feat/change-detect-45",
		"# branch.ab +1 -2",
		"1 .M N... 100644 100644 100644 b237099c b237099c internal/ui/highlight.go",
		"2 RM N... 100644 100644 100644 11b49b6e 11b49b6e R100 READTHIS.md",
		// The rename's original path, in its own record. "? notes.txt" is a
		// legal filename and it is shaped exactly like an untracked-file
		// record, so a parser that does not skip this record reports a path
		// that was renamed away as if it were still there.
		"? notes.txt",
		"? sp ace/new file.txt",
	}, "\x00") + "\x00"

	got := probedPaths(status)
	want := []string{
		"internal/ui/highlight.go",
		"READTHIS.md",
		"sp ace/new file.txt",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d paths %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// Nothing in the probe may panic on output it did not expect: a truncated
// record, an unknown record type, or an empty string.
func TestProbedPaths_SurvivesMalformedRecords(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"", "\x00", "1", "1 ", "2 RM N...", "x whatever", "? ", "#", "1 .M N... 100644",
	} {
		_ = probedPaths(in) // must not panic
	}
}

// One process now stands in for eight, so it has to notice everything those
// eight did: the file list's contents and staged flags, and the header's
// branch, ahead and behind. A change this misses is a screen that silently
// stops updating.
func TestProbe_MovesForEveryKindOfChange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		do   func(t *testing.T, repo *Repo)
	}{
		{"a tracked file is edited", func(t *testing.T, r *Repo) {
			writeFile(t, r, "a.txt", "changed\n")
		}},
		{"a new untracked file appears", func(t *testing.T, r *Repo) {
			writeFile(t, r, "new.txt", "hello\n")
		}},
		{"an untracked file appears in a new directory", func(t *testing.T, r *Repo) {
			writeFile(t, r, "deep/nested/new.txt", "hello\n")
		}},
		{"a tracked file is deleted", func(t *testing.T, r *Repo) {
			if err := os.Remove(filepath.Join(r.Dir(), "a.txt")); err != nil {
				t.Fatal(err)
			}
		}},
		{"a change is staged", func(t *testing.T, r *Repo) {
			writeFile(t, r, "a.txt", "changed\n")
			testutil.GitIn(t, r.Dir(), "add", "a.txt")
		}},
		{"a commit is made", func(t *testing.T, r *Repo) {
			writeFile(t, r, "a.txt", "changed\n")
			testutil.GitIn(t, r.Dir(), "add", "a.txt")
			testutil.GitIn(t, r.Dir(), "commit", "-m", "second")
		}},
		{"the branch changes", func(t *testing.T, r *Repo) {
			testutil.GitIn(t, r.Dir(), "checkout", "-b", "other")
		}},
		{"a file is renamed", func(t *testing.T, r *Repo) {
			testutil.GitIn(t, r.Dir(), "mv", "a.txt", "b.txt")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := setupTestRepo(t)
			writeFile(t, repo, "a.txt", "one\n")
			testutil.GitIn(t, repo.Dir(), "add", "a.txt")
			testutil.GitIn(t, repo.Dir(), "commit", "-m", "first")

			before, err := repo.Probe()
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}
			tc.do(t, repo)
			after, err := repo.Probe()
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}
			if before == after {
				t.Errorf("the probe did not move: %q", before)
			}
		})
	}
}

// The tick used to ask about the upstream separately, with two more processes.
// It does not any more: the probe gates that call too, so if ahead/behind could
// move without moving the probe the header would quietly go stale. It cannot —
// porcelain v2's --branch carries "# branch.ab +N -M".
func TestProbe_MovesWhenTheUpstreamMovesAhead(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	remote := testutil.NewBareRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	testutil.GitIn(t, repo.Dir(), "add", "a.txt")
	testutil.GitIn(t, repo.Dir(), "commit", "-m", "first")
	testutil.GitIn(t, repo.Dir(), "remote", "add", "origin", remote)
	testutil.GitIn(t, repo.Dir(), "push", "-u", "origin", "HEAD")

	before, err := repo.Probe()
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !strings.Contains(before, "branch.ab") {
		t.Fatalf("no upstream in the probe, so this test proves nothing: %q", before)
	}

	writeFile(t, repo, "a.txt", "two\n")
	testutil.GitIn(t, repo.Dir(), "add", "a.txt")
	testutil.GitIn(t, repo.Dir(), "commit", "-m", "second")

	after, err := repo.Probe()
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if before == after {
		t.Error("committing left the probe unchanged, so the header's ahead count would go stale")
	}
	if !strings.Contains(after, "branch.ab +1 -0") {
		t.Errorf("probe does not report being one commit ahead: %q", after)
	}
}

// Why the probe asks for --untracked-files=all.
//
// git's default is "normal", which collapses an untracked directory to one
// entry: "? build/" however many files are under it. Editing a file inside
// such a directory then changes nothing git reports, and the directory's own
// mtime does not move for a content edit either — so the change would be
// invisible to both halves of the fingerprint. With "all", every file is
// listed and stat'ed on its own.
//
// It matters because the file list shows untracked files individually too:
// UntrackedFiles uses ls-files --others, which never collapses.
func TestProbe_NoticesAnEditInsideAnUntrackedDirectory(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	testutil.GitIn(t, repo.Dir(), "add", "a.txt")
	testutil.GitIn(t, repo.Dir(), "commit", "-m", "first")

	// A directory git has never seen, with a file already in it.
	writeFile(t, repo, "build/out.js", "one\n")
	before, err := repo.Probe()
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !strings.Contains(before, "build/out.js") {
		t.Fatalf("the probe collapsed the untracked directory: %q", before)
	}

	// Editing it without changing anything else about the tree.
	writeFile(t, repo, "build/out.js", "one\ntwo\n")
	after, err := repo.Probe()
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if before == after {
		t.Error("an edit inside an untracked directory left the probe unchanged")
	}
}
