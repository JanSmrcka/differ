package git

import (
	"os"
	"os/exec"
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
var probeFormat = []string{
	// --no-optional-locks or the probe fights the user for the index. git
	// status opportunistically rewrites .git/index to refresh its stat cache,
	// which takes index.lock — measured at 8 failed `git add`s in 120 while
	// probing in a loop, and ~1% at the real interval. The commands this
	// replaced never wrote the index, so that contention would have been new,
	// and it lands on exactly differ's user: an agent running git in the same
	// repository while differ watches it.
	"--no-optional-locks",
	"status", "--porcelain=v2", "--branch", "--untracked-files=all", "-z",
}

// Probe returns a fingerprint of everything that could make the file list, the
// header or the diff different. Equal fingerprints mean nothing moved.
//
// ref is what differ is comparing against, empty for the working tree. It
// costs a second process, because git status describes the worktree against
// the index and HEAD and says nothing about any other ref — so under `-r main`
// the fingerprint would not move when main did, and the screen would freeze
// for as long as the session lasted.
func (r *Repo) Probe(ref string) (string, error) {
	status, err := r.run(probeFormat...)
	if err != nil {
		return "", err
	}
	if ref != "" {
		// Errors are folded in rather than returned: a ref that does not
		// resolve is a stable answer, and it becomes unstable the moment the
		// ref appears.
		oid, err := r.run("rev-parse", "--verify", "--quiet", ref+"^{commit}")
		if err != nil {
			oid = "unresolved:" + err.Error()
		}
		status += "\x00ref:" + ref + "\x00" + strings.TrimSpace(oid)
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
	for _, p := range probedPaths(status) {
		b.WriteString("\x00")
		b.WriteString(p.path)
		full := filepath.Join(r.Dir(), p.path)

		if p.gitlink {
			// A submodule's stat says nothing: committing inside it changes
			// neither the directory's mtime nor the gitlink oids status
			// reports, which are the superproject's *recorded* commit and stay
			// put. Only the second and later moves are invisible, which is
			// worse than never noticing at all. Ask it where its HEAD is.
			head, err := runIn(full, "rev-parse", "HEAD")
			if err != nil {
				head = "unreadable"
			}
			b.WriteString("\x00sub:")
			b.WriteString(strings.TrimSpace(head))
			continue
		}

		// Lstat, not Stat: a symlink's own target is what the diff shows, and
		// following it would report the mtime of whatever it points at.
		if info, err := os.Lstat(full); err == nil {
			b.WriteString("\x00")
			b.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))
			b.WriteString("\x00")
			b.WriteString(strconv.FormatInt(info.Size(), 10))
		} else {
			// Gone between the status call and this stat. Deliberately
			// untested: reaching it needs the file to vanish inside that
			// window, and a test that could force it would be testing the
			// scheduler rather than this. Every case that *can* be staged —
			// a deleted tracked file, a removed untracked one — moves the
			// status output too, so this is defence in depth. Naming it rather
			// than skipping it keeps the fingerprint stable while it is gone,
			// so the screen settles instead of refreshing every tick.
			b.WriteString("\x00gone")
		}
	}
	return b.String(), nil
}

// runIn runs git in a directory that is not this repository — a submodule.
func runIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"--no-optional-locks"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

// probedPath is a path status named, and whether it is a submodule.
type probedPath struct {
	path    string
	gitlink bool
}

// probedPaths pulls the worktree paths out of porcelain v2's -z records.
//
// The fields are space-separated and the path is last, so a path containing
// spaces survives as long as nothing splits past the fixed field count. -z is
// what makes that safe: without it git quotes such paths instead.
func probedPaths(status string) []probedPath {
	// Fields before the path, by record type. A rename ("2") also emits the
	// original path as the *next* record, which is skipped: it no longer exists
	// on disk, so stat'ing it would say "gone" forever.
	fieldsBefore := map[byte]int{'1': 8, '2': 9, 'u': 10, '?': 1}

	var paths []probedPath
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
		// The worktree file mode is field 5 on the records that carry one.
		// 160000 is a gitlink: a submodule, whose contents no stat describes.
		gitlink := n > 5 && parts[5] == "160000"
		paths = append(paths, probedPath{path: parts[n], gitlink: gitlink})
		skipNext = rec[0] == '2'
	}
	return paths
}
