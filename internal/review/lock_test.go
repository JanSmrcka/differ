package review

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The whole point: a second differ in the same repository does not get to
// write. Two instances each serialised their entire session over review.json
// with no merge, so the second save destroyed the first's comments — and
// brought back a comment the first had already delivered, which would send
// the agent a review it had acted on.
func TestLock_ASecondProcessIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	held, err := TakeLock(dir)
	if err != nil {
		t.Fatalf("the first claim failed: %v", err)
	}
	defer held.Release()

	// pid 1 stands in for another live differ: it is always running and is
	// never this process.
	writeLock(t, dir, "1")

	if _, err := TakeLock(dir); !errors.Is(err, ErrHeldElsewhere) {
		t.Errorf("a lock held by a live process gave %v, want ErrHeldElsewhere", err)
	}
}

// A kill is the case the whole feature exists for, so it must not be the case
// that locks the reviewer out of their own review.
func TestLock_ALockLeftByADeadProcessIsTakenOver(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A pid that cannot be running: the kernel would have to have allocated
	// it, and it is above every default pid_max.
	writeLock(t, dir, "4194303")

	lock, err := TakeLock(dir)
	if err != nil {
		t.Fatalf("a stale lock was believed: %v", err)
	}
	defer lock.Release()

	if got := lockHolder(filepath.Join(dir, "differ", lockName)); got != os.Getpid() {
		t.Errorf("the lock names pid %d, want this process (%d)", got, os.Getpid())
	}
}

// Illegible is not the same as held. A truncated or empty lock — a kill
// between create and write — names nobody, and believing it would lock the
// repository out permanently.
func TestLock_AnIllegibleLockIsTakenOver(t *testing.T) {
	t.Parallel()
	for _, content := range []string{"", "\n", "not a pid", "0"} {
		dir := t.TempDir()
		writeLock(t, dir, content)

		lock, err := TakeLock(dir)
		if err != nil {
			t.Errorf("a lock reading %q was believed: %v", content, err)
			continue
		}
		lock.Release()
	}
}

// Releasing gives the next differ its turn.
func TestLock_ReleasingLetsTheNextOneIn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	first, err := TakeLock(dir)
	if err != nil {
		t.Fatalf("TakeLock: %v", err)
	}
	first.Release()

	if _, err := os.Stat(filepath.Join(dir, "differ", lockName)); !os.IsNotExist(err) {
		t.Errorf("the lock file is still there after Release: %v", err)
	}
	second, err := TakeLock(dir)
	if err != nil {
		t.Fatalf("the second claim failed after a release: %v", err)
	}
	second.Release()
}

// Release must not remove a lock that now belongs to somebody else: this
// process may have been believed dead and its claim taken over.
func TestLock_ReleasingSomebodyElsesClaimDoesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	lock, err := TakeLock(dir)
	if err != nil {
		t.Fatalf("TakeLock: %v", err)
	}
	writeLock(t, dir, "1")
	lock.Release()

	if got := lockHolder(filepath.Join(dir, "differ", lockName)); got != 1 {
		t.Errorf("Release removed another process's claim: holder is now %d", got)
	}
}

// A nil lock is what a differ with nowhere to keep its review holds.
func TestLock_ReleasingNothingIsSafe(t *testing.T) {
	t.Parallel()
	var lock *Lock
	lock.Release()
}

func writeLock(t *testing.T, gitDir, content string) {
	t.Helper()
	dir := filepath.Join(gitDir, "differ")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, lockName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
