package git

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Detecting that the repository moved, cheaply enough to ask often.
//
// The poll loop used to rebuild everything every two seconds: eight git
// processes — HasCommits, two name-status, two num-stat, ls-files, and two for
// the upstream count — whether or not anything had happened. Measured on this
// repository that is 112 ms of work every tick, and almost none of it is work:
// `git --version` costs 13.6 ms here against `git status`'s 15.6, so roughly
// fourteen of every sixteen milliseconds is spent starting a process.
//
// That is why this is not a filesystem watcher. A watcher removes the ~2 ms of
// real work and leaves the ~14 ms of process startup in place for every refresh
// that does happen, in exchange for a dependency, a file descriptor per
// directory under kqueue on macOS, and a walk of the whole tree to install the
// watches — the walk the issue asks us to avoid. The cost here is processes, so
// the fix is to start fewer of them.

// probeFormat is one git process standing in for eight. Beyond the paths and
// their staged/unstaged states it carries branch.oid, branch.head,
// branch.upstream and branch.ab, so a commit, a checkout, a rebase and a fetch
// all move the fingerprint without anyone asking about them separately.
var probeFormat = []string{"status", "--porcelain=v2", "--branch", "--untracked-files=all", "-z"}

// Probe returns a fingerprint of everything that could make the file list, the
// header or the diff different. Equal fingerprints mean nothing moved.
func (r *Repo) Probe() (string, error) {
	status, err := r.run(probeFormat...)
	if err != nil {
		return "", err
	}

	// Status is not enough on its own. It reports a modified file as
	// "1 .M <mode…> <hH> <hI> path", and those two object ids are HEAD's and
	// the index's — the working tree's content is never hashed. So editing a
	// file that was already modified produces byte-identical output, which is
	// the commonest thing that happens while an agent is working. Each named
	// path is stat'ed to fill that gap: no read, no subprocess, and the same
	// signal git itself uses to decide a file is dirty.
	var b strings.Builder
	b.WriteString(status)
	for _, path := range probedPaths(status) {
		b.WriteString("\x00")
		b.WriteString(path)
		if info, err := os.Lstat(filepath.Join(r.Dir(), path)); err == nil {
			b.WriteString("\x00")
			b.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))
			b.WriteString("\x00")
			b.WriteString(strconv.FormatInt(info.Size(), 10))
		} else {
			// Gone between status and the stat. That is a change in itself, and
			// naming it keeps the fingerprint stable while it stays gone.
			b.WriteString("\x00gone")
		}
	}
	return b.String(), nil
}

// probedPaths pulls the worktree paths out of porcelain v2's -z records.
//
// The fields are space-separated and the path is last, so a path containing
// spaces survives as long as nothing splits past the fixed field count. -z is
// what makes that safe: without it git quotes such paths instead.
func probedPaths(status string) []string {
	// Fields before the path, by record type. A rename ("2") also emits the
	// original path as the *next* record, which is skipped: it no longer exists
	// on disk, so stat'ing it would say "gone" forever.
	fieldsBefore := map[byte]int{'1': 8, '2': 9, 'u': 10, '?': 1}

	var paths []string
	skipNext := false
	for _, rec := range strings.Split(status, "\x00") {
		if rec == "" {
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		n, ok := fieldsBefore[rec[0]]
		if !ok || len(rec) < 2 || rec[1] != ' ' {
			continue // a "# branch.*" header, or something unrecognised
		}
		parts := strings.SplitN(rec, " ", n+1)
		if len(parts) != n+1 {
			continue
		}
		paths = append(paths, parts[n])
		skipNext = rec[0] == '2'
	}
	return paths
}
