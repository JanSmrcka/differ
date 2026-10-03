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
// the upstream count — whether or not anything had happened. Most of that is
// not work: measured over 200 iterations on this machine, `git --version`
// costs 8.5 ms against this probe's 13.2, and a bare fork/exec of `true` is
// 1.5 ms. So roughly two thirds of each call is spent getting a process to the
// point of doing anything.
//
// An earlier version of this comment said 13.6 ms and 15.6 ms — a 14-in-16
// ratio — from a ten-iteration sample. The conclusion survives the correction;
// the arithmetic did not.
//
// This is not a filesystem watcher, but not because of that arithmetic: a
// watcher's whole point is the idle ticks, where it would cost nothing against
// this probe's one process a second. It is rejected for the reasons that stand
// on their own — a dependency CLAUDE.md restricts, a file descriptor per
// directory under kqueue on macOS since fsnotify does not watch recursively,
// and a walk of the whole tree to install the watches, which is the walk the
// issue asks us to avoid. One cheap process a second is a price worth paying
// to not own any of that.

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
			// put. Only the second and later moves were invisible, which is
			// worse than never noticing at all.
			b.WriteString("\x00sub:")
			b.WriteString(submoduleHead(full))
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

// submoduleHead reads where a submodule's HEAD points, without starting a
// process.
//
// `git -C <sub> rev-parse HEAD` is the obvious way and it was the first one,
// but it is a fork per submodule per tick: ten dirty submodules measured 184 ms
// against 11 ms, every second, and "dirty" includes one that merely has an
// untracked file inside — so the cost is permanent rather than transient. The
// files it would read are two small ones, and reading them directly costs
// nothing.
//
// The exact value does not matter, only that it moves when the submodule does.
// An unreadable layout returns a constant, which is stable rather than noisy.
func submoduleHead(dir string) string {
	gitdir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitdir)
	if err != nil {
		return "absent"
	}
	if !info.IsDir() {
		// A worktree or a submodule cloned the modern way: .git is a file
		// holding "gitdir: <path>", relative to the submodule.
		raw, err := os.ReadFile(gitdir)
		if err != nil {
			return "unreadable"
		}
		pointer := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), "gitdir:"))
		if pointer == "" {
			return "unreadable"
		}
		if !filepath.IsAbs(pointer) {
			pointer = filepath.Join(dir, pointer)
		}
		gitdir = pointer
	}

	head, err := os.ReadFile(filepath.Join(gitdir, "HEAD"))
	if err != nil {
		return "unreadable"
	}
	text := strings.TrimSpace(string(head))
	ref, isSymbolic := strings.CutPrefix(text, "ref:")
	if !isSymbolic {
		return text // detached: HEAD is the object id itself
	}
	ref = strings.TrimSpace(ref)

	// A loose ref is a file holding the object id. A packed one is not, and
	// rather than parse packed-refs the file's absence is folded in with the
	// branch name — which still moves on a branch switch, and packed refs do
	// not move under a working session.
	oid, err := os.ReadFile(filepath.Join(gitdir, filepath.FromSlash(ref)))
	if err != nil {
		return "packed:" + ref
	}
	return ref + ":" + strings.TrimSpace(string(oid))
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
		// 160000 is a gitlink: a submodule, whose contents no stat describes.
		//
		// Field 5 is the worktree mode on "1" and "2" records. On "u" it is
		// the stage-3 mode — the worktree mode is field 6 there — but a
		// conflicted submodule carries 160000 in both, so one index reads all
		// three record types. Nothing here needs the mode for anything else.
		gitlink := n > 5 && parts[5] == "160000"
		paths = append(paths, probedPath{path: parts[n], gitlink: gitlink})
		skipNext = rec[0] == '2'
	}
	return paths
}
