package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jansmrcka/differ/internal/testutil"
)

// The probe is what the poll loop asks "did anything move?" — so two calls with
// nothing happening in between must agree, or every tick would rebuild the file
// list and reload the diff.
func TestProbe_IsStableWhenNothingChanges(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	writeFile(t, repo, "a.txt", "one\n")

	first, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	second, err := repo.Probe("")
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
	first, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	writeFile(t, repo, "a.txt", "one\ntwo\nthree\n")
	second, err := repo.Probe("")
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
		t.Fatalf("got %d paths %+v, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i].path != want[i] {
			t.Errorf("path %d = %q, want %q", i, got[i].path, want[i])
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

			before, err := repo.Probe("")
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}
			tc.do(t, repo)
			after, err := repo.Probe("")
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

	before, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !strings.Contains(before, "branch.ab") {
		t.Fatalf("no upstream in the probe, so this test proves nothing: %q", before)
	}

	writeFile(t, repo, "a.txt", "two\n")
	testutil.GitIn(t, repo.Dir(), "add", "a.txt")
	testutil.GitIn(t, repo.Dir(), "commit", "-m", "second")

	after, err := repo.Probe("")
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
	before, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !strings.Contains(before, "build/out.js") {
		t.Fatalf("the probe collapsed the untracked directory: %q", before)
	}

	// Editing it without changing anything else about the tree.
	writeFile(t, repo, "build/out.js", "one\ntwo\n")
	after, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if before == after {
		t.Error("an edit inside an untracked directory left the probe unchanged")
	}
}

// Under -r the screen showed a comparison against a ref, and git status says
// nothing about any ref but HEAD. So the fingerprint never moved when the ref
// did, and the refresh was gated off permanently — a teammate's push you
// fetched, a sibling worktree committing, a rebase of the base branch, all
// invisible for as long as the session lasted. It is the worst failure this
// design can have: the screen stops updating and says nothing.
func TestProbe_MovesWhenTheRefItComparesAgainstMoves(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	testutil.GitIn(t, repo.Dir(), "add", "a.txt")
	testutil.GitIn(t, repo.Dir(), "commit", "-m", "first")
	testutil.GitIn(t, repo.Dir(), "branch", "base")
	testutil.GitIn(t, repo.Dir(), "checkout", "-q", "-b", "work")

	before, err := repo.Probe("base")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	// base moves under us, with the working tree untouched.
	testutil.GitIn(t, repo.Dir(), "checkout", "-q", "base")
	writeFile(t, repo, "b.txt", "two\n")
	testutil.GitIn(t, repo.Dir(), "add", "b.txt")
	testutil.GitIn(t, repo.Dir(), "commit", "-m", "on base")
	testutil.GitIn(t, repo.Dir(), "checkout", "-q", "work")

	after, err := repo.Probe("base")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if before == after {
		t.Error("the ref moved and the probe did not — the screen would never refresh again")
	}

	// And with no ref the probe must not start asking about one.
	plain, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if strings.Contains(plain, "ref:") {
		t.Errorf("the working-tree probe mentions a ref: %q", plain)
	}
}

// A ref that does not exist is a stable answer, and becomes unstable the
// moment it appears.
func TestProbe_AnUnresolvableRefIsStableUntilItExists(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	testutil.GitIn(t, repo.Dir(), "add", "a.txt")
	testutil.GitIn(t, repo.Dir(), "commit", "-m", "first")

	first, err := repo.Probe("nosuchref")
	if err != nil {
		t.Fatalf("an unresolvable ref made the probe fail: %v", err)
	}
	second, err := repo.Probe("nosuchref")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if first != second {
		t.Error("an unresolvable ref gave two different answers")
	}

	testutil.GitIn(t, repo.Dir(), "branch", "nosuchref")
	third, err := repo.Probe("nosuchref")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if third == first {
		t.Error("the ref appeared and the probe did not move")
	}
}

// The probe must not fight the user for the index.
//
// git status rewrites .git/index to refresh its stat cache, which takes
// index.lock — measured at 8 failed `git add`s in 120 while probing in a loop.
// The eight commands this replaced never wrote the index, so the contention
// would have been new, and it lands on exactly differ's user: an agent running
// git in the same repository while differ watches it.
func TestProbe_DoesNotTakeTheIndexLock(t *testing.T) {
	t.Parallel()
	found := false
	for _, arg := range probeFormat {
		if arg == "--no-optional-locks" {
			found = true
		}
		if arg == "status" && !found {
			t.Fatal("--no-optional-locks must come before the subcommand")
		}
	}
	if !found {
		t.Error("the probe may rewrite .git/index and make the user's git commands fail")
	}

	// And it still has to work with the lock held by someone else.
	repo := setupTestRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	lock := filepath.Join(repo.Dir(), ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(lock) }()

	if _, err := repo.Probe(""); err != nil {
		t.Errorf("the probe failed while another git process held the lock: %v", err)
	}
}

// Each half of the stat, on its own. Removing the stat wholesale is caught by
// the re-edit test, but size, mtime and the gone marker were individually
// unprotected.
func TestProbe_TheStatNoticesSizeAndTimeSeparately(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		first string
		then  string
	}{
		// Same length, different bytes: only the mtime can tell these apart.
		{"same size, different content", "aaa\n", "bbb\n"},
		// Different length: size alone is enough.
		{"different size", "aaa\n", "aaaa\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := setupTestRepo(t)
			writeFile(t, repo, "a.txt", tc.first)
			testutil.GitIn(t, repo.Dir(), "add", "a.txt")
			testutil.GitIn(t, repo.Dir(), "commit", "-m", "first")
			writeFile(t, repo, "a.txt", tc.first+"x\n")

			before, err := repo.Probe("")
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}
			writeFile(t, repo, "a.txt", tc.then+"x\n")
			after, err := repo.Probe("")
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}
			if before == after {
				t.Error("the probe did not move")
			}
		})
	}
}

// A file that disappears between the status and the stat is a change, and
// stays one stable answer while it is gone.
func TestProbe_AVanishedPathIsNamedRatherThanIgnored(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	testutil.GitIn(t, repo.Dir(), "add", "a.txt")
	testutil.GitIn(t, repo.Dir(), "commit", "-m", "first")
	writeFile(t, repo, "gone.txt", "two\n")

	before, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !strings.Contains(before, "gone.txt") {
		t.Fatalf("the untracked file is not in the probe: %q", before)
	}
	if err := os.Remove(filepath.Join(repo.Dir(), "gone.txt")); err != nil {
		t.Fatal(err)
	}
	after, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if before == after {
		t.Error("a file disappearing left the probe unchanged")
	}
}

// Unmerged records. This is the one record type whose status output never
// changes as you work — the three stage object ids are fixed for the duration
// of the conflict — so the whole signal rests on parsing its path correctly
// and lstat'ing it. Get the field count wrong and a conflict-resolution
// session silently stops updating.
func TestProbe_NoticesEditsToAConflictedFile(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	writeFile(t, repo, "a.txt", "base\n")
	testutil.GitIn(t, repo.Dir(), "add", "a.txt")
	testutil.GitIn(t, repo.Dir(), "commit", "-m", "base")
	testutil.GitIn(t, repo.Dir(), "checkout", "-q", "-b", "other")
	writeFile(t, repo, "a.txt", "theirs\n")
	testutil.GitIn(t, repo.Dir(), "commit", "-qam", "theirs")
	testutil.GitIn(t, repo.Dir(), "checkout", "-q", "master")
	writeFile(t, repo, "a.txt", "ours\n")
	testutil.GitIn(t, repo.Dir(), "commit", "-qam", "ours")
	_ = testutil.GitInAllowFail(t, repo.Dir(), "merge", "other")

	before, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !strings.Contains(before, "\x00u ") {
		t.Skipf("no unmerged record produced; probe was %q", before)
	}

	// Resolving it by hand, without staging.
	writeFile(t, repo, "a.txt", "resolved\n")
	after, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if before == after {
		t.Error("editing a conflicted file left the probe unchanged")
	}
}

// Submodules. porcelain v2 reports a dirty submodule as "1 .M SC.. 160000 …"
// and the two object ids are the superproject's *recorded* commit, which stays
// put however many commits land inside. The directory's mtime and size do not
// move either. So the first pointer move was visible (the record appears) and
// every later one was not, which is worse than never noticing at all.
func TestProbe_MovesWhenASubmodulesHeadMovesAgain(t *testing.T) {
	t.Parallel()
	sub := setupTestRepo(t)
	writeFile(t, sub, "s.txt", "one\n")
	testutil.GitIn(t, sub.Dir(), "add", "s.txt")
	testutil.GitIn(t, sub.Dir(), "commit", "-m", "sub first")

	super := setupTestRepo(t)
	writeFile(t, super, "a.txt", "one\n")
	testutil.GitIn(t, super.Dir(), "add", "a.txt")
	testutil.GitIn(t, super.Dir(), "commit", "-m", "first")
	out := testutil.GitInAllowFail(t, super.Dir(), "-c", "protocol.file.allow=always",
		"submodule", "add", sub.Dir(), "sub")
	if !super.hasPath("sub") {
		t.Skipf("submodule add did not work here: %s", out)
	}
	testutil.GitIn(t, super.Dir(), "commit", "-m", "add submodule")

	commitInSub := func(body string) {
		dir := filepath.Join(super.Dir(), "sub")
		testutil.GitIn(t, dir, "config", "user.email", "t@t")
		testutil.GitIn(t, dir, "config", "user.name", "t")
		if err := os.WriteFile(filepath.Join(dir, "s.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		testutil.GitIn(t, dir, "commit", "-qam", body)
	}

	commitInSub("two\n")
	first, err := super.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	// The second move is the one that used to be invisible.
	commitInSub("three\n")
	second, err := super.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if first == second {
		t.Error("a second commit inside the submodule left the probe unchanged")
	}
}

// Lstat, not Stat. What the diff shows for a symlink is the link's own target
// text, so the link is what has to be watched.
//
// Isolating that takes some care. The link is already modified before the
// change, so git status says the same thing either way; the two candidate
// targets are the same length and are given the same mtime, so following the
// link sees nothing move. Only the link's own mtime does.
func TestProbe_WatchesASymlinkRatherThanWhatItPointsAt(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	writeFile(t, repo, "one.txt", "1\n")
	writeFile(t, repo, "two.txt", "2\n")
	writeFile(t, repo, "thr.txt", "3\n")
	link := filepath.Join(repo.Dir(), "link")
	if err := os.Symlink("one.txt", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	testutil.GitIn(t, repo.Dir(), "add", "-A")
	testutil.GitIn(t, repo.Dir(), "commit", "-m", "first")

	// Same length and the same timestamp, so Stat cannot tell them apart.
	stamp := time.Now().Add(-time.Hour)
	for _, name := range []string{"two.txt", "thr.txt"} {
		if err := os.Chtimes(filepath.Join(repo.Dir(), name), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}

	// The link is already modified, so the status output stops moving here.
	repoint := func(target string) {
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	repoint("two.txt")
	before, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	repoint("thr.txt")
	after, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if before == after {
		t.Error("retargeting a symlink between two identical-looking files left the probe unchanged")
	}
}

// Size earns its place independently of the mtime.
//
// In ordinary use the mtime catches everything size would, so this pins the
// mtime back by hand to isolate it. The case is not academic: tar, unzip,
// cp -p and rsync -t all restore timestamps, so a file can genuinely change
// length while keeping the mtime it had.
func TestProbe_NoticesALengthChangeThatKeptItsTimestamp(t *testing.T) {
	t.Parallel()
	repo := setupTestRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	testutil.GitIn(t, repo.Dir(), "add", "a.txt")
	testutil.GitIn(t, repo.Dir(), "commit", "-m", "first")
	writeFile(t, repo, "a.txt", "one\ntwo\n")

	path := filepath.Join(repo.Dir(), "a.txt")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stamp := info.ModTime()

	before, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	writeFile(t, repo, "a.txt", "one\ntwo\nthree\n")
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if !got.ModTime().Equal(stamp) {
		t.Skipf("could not pin the mtime here (%v vs %v)", got.ModTime(), stamp)
	}

	after, err := repo.Probe("")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if before == after {
		t.Error("a file grew with its timestamp unchanged and the probe did not move")
	}
}

// Reading the submodule's HEAD must cost no processes: it is per submodule per
// tick, and `git rev-parse` there measured 184 ms against 11 ms with ten dirty
// submodules — where "dirty" includes one that merely has an untracked file
// inside, so the cost would be permanent rather than transient.
func TestProbe_ReadingASubmodulesHeadStartsNoProcess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitdir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(filepath.Join(gitdir, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(gitdir, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// On a branch, loose ref.
	write("HEAD", "ref: refs/heads/main\n")
	write(filepath.Join("refs", "heads", "main"), "aaaa1111\n")
	onBranch := submoduleHead(dir)
	if !strings.Contains(onBranch, "aaaa1111") {
		t.Errorf("did not read the branch's object id: %q", onBranch)
	}

	// The branch moves.
	write(filepath.Join("refs", "heads", "main"), "bbbb2222\n")
	if moved := submoduleHead(dir); moved == onBranch {
		t.Error("the submodule's branch moved and the reading did not")
	}

	// Detached: HEAD is the object id.
	write("HEAD", "cccc3333\n")
	if got := submoduleHead(dir); got != "cccc3333" {
		t.Errorf("detached HEAD read as %q", got)
	}

	// A packed ref has no file; the answer must be stable rather than noisy.
	write("HEAD", "ref: refs/heads/packed\n")
	first, second := submoduleHead(dir), submoduleHead(dir)
	if first != second {
		t.Errorf("a packed ref gave two answers: %q and %q", first, second)
	}
	if first == "" {
		t.Error("a packed ref read as nothing at all")
	}
}

// A .git file pointing elsewhere is how submodules are cloned now, and how
// worktrees are laid out.
func TestProbe_FollowsAGitdirPointerFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	real := filepath.Join(root, "modules", "sub")
	if err := os.MkdirAll(filepath.Join(real, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "refs", "heads", "main"), []byte("dddd4444\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Relative, the way git writes it.
	if err := os.WriteFile(filepath.Join(sub, ".git"), []byte("gitdir: ../modules/sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := submoduleHead(sub); !strings.Contains(got, "dddd4444") {
		t.Errorf("did not follow the gitdir pointer: %q", got)
	}
	// And an absent one is stable, not an error.
	if got := submoduleHead(filepath.Join(root, "nothing")); got != "absent" {
		t.Errorf("a missing submodule read as %q", got)
	}
}
