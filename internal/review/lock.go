package review

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// One differ owns a repository's review file.
//
// Every save serialises the whole session and renames it over review.json:
// there is no merge, and the last writer wins the file. Two differs in one
// repository — one per tmux pane is the ordinary way to use it, and `-s` in
// one pane against plain in another is the ordinary way to compare — silently
// destroyed each other's comments, and worse, brought back an
// already-delivered comment that the other instance had moved into history.
// Sending the agent a review it has already acted on is the one failure this
// file must not introduce.
//
// A merge would have to decide what two edits of the same comment mean, and
// resurrecting a sent comment is exactly what a naive one would do. So the
// second instance does not write: it reviews normally and says so, which
// costs a restart and never costs a comment.

// lockName is the file that says which process owns the review.
const lockName = "review.lock"

// ErrHeldElsewhere says another differ already owns this repository's review.
var ErrHeldElsewhere = errors.New("another differ already has this repository's review open")

// Lock is one process's claim on a repository's review file.
type Lock struct{ path string }

// TakeLock claims the review for this process, or reports who has it.
//
// The claim is a file created with O_EXCL holding this process's pid. A
// process that was killed leaves one behind, so a lock naming a pid that is
// no longer running is taken over rather than believed: a crash is precisely
// the case this whole feature exists for, and it must not be the case that
// locks the reviewer out.
func TakeLock(gitDir string) (*Lock, error) {
	dir := filepath.Join(gitDir, "differ")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, lockName)

	for attempt := range 2 {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			if err := f.Close(); err != nil {
				return nil, err
			}
			return &Lock{path: path}, nil
		}
		if !os.IsExist(err) || attempt > 0 {
			return nil, err
		}
		// Our own pid is not somebody else. Only one Model exists at a time
		// in a differ process, so a lock naming this process is one this
		// process is finished with — or one Close did not reach, which is
		// still not a reason to refuse the reviewer their own review.
		if pid := lockHolder(path); pid > 0 && pid != os.Getpid() && processAlive(pid) {
			return nil, fmt.Errorf("%w (pid %d)", ErrHeldElsewhere, pid)
		}
		// Nobody is holding it. Remove it and try once more; if another
		// differ wins the race in between, the second attempt reports it.
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nil, ErrHeldElsewhere
}

// Release gives the claim up. Best effort: a lock left behind by a kill is
// taken over by the next start, which is the whole reason the pid is in it.
func (l *Lock) Release() {
	if l == nil {
		return
	}
	// Only if it is still ours. Another differ may have taken over a lock we
	// were believed dead for, and removing theirs would be worse than
	// leaving ours.
	if lockHolder(l.path) == os.Getpid() {
		_ = os.Remove(l.path)
	}
}

// lockHolder reads the pid out of a lock file, or 0.
func lockHolder(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// processAlive reports whether a pid names a running process.
//
// Signal 0 is the portable "does this exist and may I signal it?" — it
// delivers nothing. EPERM counts as alive: the process is there, it just
// belongs to somebody else.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
