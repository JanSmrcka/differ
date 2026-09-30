package ui

import (
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jansmrcka/differ/internal/git"
)

// Noticing that a file moved under the reviewer.
//
// The signal has to be quiet above all: a false "changed" un-reviews a file
// the user did read, and the ratio in the status bar goes backwards for no
// reason they can see. That rules out anything that fires on a write rather
// than on an edit.

// fileKeysOf fingerprints each changed file, one key per path.
//
// The key is the *content differ is showing*, which is what "changed under the
// reviewer" has to mean:
//
//   - Normally, and under -r, the diff runs to the working tree, so the key is
//     a hash of the worktree file. Staging does not touch it, so `git add` —
//     or the user pressing `a` — cannot flag a file, and a reformat that
//     writes the same bytes back is not a change either. mtime and size were
//     tried first and got both of those wrong: `gofmt -w`, `prettier --write`
//     and `git checkout -- .` all rewrite identical bytes and flagged every
//     file the reviewer had read.
//   - Under -s the diff runs to the index, so the key is git's own object id
//     for the staged content. The line counts cannot answer that question:
//     staging moves lines between the staged and unstaged halves of a file
//     without changing their total.
//
// Reading the changed files is not new cost of a kind this program does not
// already pay — buildFileItems reads every untracked file in full on the same
// refresh.
func fileKeysOf(repo *git.Repo, files []fileItem, stagedOnly bool) map[string]string {
	var staged map[string]string
	indexReadable := true
	if stagedOnly && repo != nil {
		// One call for the whole index, not one per file.
		var err error
		staged, err = repo.IndexHashes()
		indexReadable = err == nil
	}

	keys := make(map[string]string, len(files))
	for _, f := range files {
		path := f.change.Path
		if _, done := keys[path]; done {
			// git reports a file with both staged and unstaged changes twice.
			// One content fingerprint covers both halves.
			continue
		}
		if stagedOnly {
			keys[path] = indexKey(staged, path, indexReadable)
			continue
		}
		keys[path] = worktreeKey(repo, path)
	}
	return keys
}

// indexKey is a path's staged object id, or a sentinel saying why there is
// none.
//
// The sentinels matter. "index:" with nothing after it was the answer both for
// a staged deletion — which then could never look changed — and for every path
// in the changeset when IndexHashes failed, which switched change detection off
// for the whole session with nothing on screen to say so. worktreeKey has
// always distinguished its failures this way; this half did not.
func indexKey(staged map[string]string, path string, readable bool) string {
	if !readable {
		return "index-unreadable"
	}
	oid, ok := staged[path]
	if !ok {
		return "index-absent"
	}
	return "index:" + oid
}

// worktreeKey fingerprints a file's content on disk.
//
// A file that is gone — deleted in the working tree — has no content to hash,
// and says so rather than falling back to something that looks like a hash.
func worktreeKey(repo *git.Repo, path string) string {
	if repo == nil {
		return "unknown"
	}
	file, err := os.Open(filepath.Join(repo.Dir(), path))
	if err != nil {
		return "gone"
	}
	// Read-only, so there is nothing a close error could tell us.
	defer func() { _ = file.Close() }()

	h := fnv.New64a()
	size, err := io.Copy(h, file)
	if err != nil {
		return "unreadable"
	}
	// The size is in the key as well, so a hash collision would have to be a
	// collision between two files of exactly the same length.
	return fmt.Sprintf("%d:%x", size, h.Sum64())
}

// noteChangedFiles tells the session which files moved since the last refresh.
//
// A path missing from the previous fingerprints counts as moved too: a file
// the agent committed leaves the changeset and comes back rewritten, which is
// the ordinary agent loop, and requiring a previous key meant that case was
// never flagged. NoteChange is a no-op for a file the user never read, so a
// genuinely new file stays quiet.
//
// The file under the cursor is excluded: whatever arrives for it is what the
// user is looking at, so it cannot be out of date to its own reader. Saying
// that the visible diff moved is a different job (#46).
//
// The result is read by the status bar's progress readout. The per-file badge
// in the changed-file list is #52.
func (m Model) noteChangedFiles(keys map[string]string) Model {
	if keys == nil {
		// Nothing was fingerprinted, so nothing is known about what moved.
		// Adopting an empty map would make every file look new on the next
		// refresh and quietly switch detection off for the rest of the run.
		return m
	}
	if m.session != nil && m.fileKeys != nil {
		current := m.currentFilePath()
		for path, key := range keys {
			if m.fileKeys[path] != key && path != current {
				m.session.NoteChange(path)
			}
		}
	}
	m.fileKeys = keys
	return m
}

// diffStale reports whether the diff on screen is older than the file it came
// from.
//
// Derived, never stored. The renderer records the content key it was built
// from; if the file's key has moved on, what is on screen is out of date. That
// cannot get stuck, cannot survive the renderer being replaced, and cannot
// describe a file the reviewer has left — all of which a boolean flag did.
func (m Model) diffStale() bool {
	if m.rendererKey == "" || m.renderer == nil {
		return false
	}
	// Only about the file actually on screen. After n or p the renderer is
	// still the previous file's until its diff arrives.
	if m.rendererPath != m.currentFilePath() {
		return false
	}
	now, known := m.fileKeys[m.rendererPath]
	if !known {
		// The file has left the changeset. That is the file list's business,
		// not a reason to offer a reload of something that is no longer there.
		return false
	}
	return now != m.rendererKey
}

// holdsTheDiff reports whether the content on screen belongs to someone who
// would rather be asked before it changes.
//
// Reviewing is close reading with comments attached to particular lines, so a
// silent swap can leave a pending comment describing something that is no
// longer there. Outside review mode differ stays live, which is the point of
// the poll.
func (m Model) holdsTheDiff() bool {
	// The renderer has to be the cursor's file, not just any file: diffs load
	// asynchronously, so right after n or p it is still the previous one.
	//
	// That last clause is defence in depth and deliberately untested. Removing
	// it leaves the suite green, and I could not build a case where it changes
	// what the user sees: diffStale carries the same guard for the notice, and
	// the two can only disagree while a navigation's own load is in flight,
	// which supersedes the reload this would have skipped. It stays because
	// the function's name is a claim about the diff on screen.
	return m.mode == modeReview && m.renderer != nil && m.rendererPath == m.currentFilePath()
}

// noteRenderedDiff records what the diff on screen was built from.
//
// key comes from the load itself, read at the same moment as the content. It
// used to be looked up in m.fileKeys, which the previous poll installed — so a
// write landing between that poll and the read left the renderer holding
// content newer than its recorded key, and the next poll announced a change
// that was already on screen.
func (m Model) noteRenderedDiff(key string) Model {
	m.rendererKey = key
	if key == "" {
		m.rendererKey = m.fileKeys[m.rendererPath]
	}
	added, gone := m.countsFor(m.files, m.rendererPath)
	m.rendererAdded, m.rendererGone = added, gone
	return m
}

// changeSince describes how the file on screen differs from the diff being
// shown, for the notice.
//
// Measured against what the renderer was built from, not against the previous
// poll. Reading the previous poll meant a reviewer who left the notice up
// through several changes was told about the last one only: four lines added
// over two refreshes reported as two.
func (m Model) changeSince(files []fileItem) string {
	nowAdded, nowGone := m.countsFor(files, m.rendererPath)
	added := nowAdded - m.rendererAdded
	removed := nowGone - m.rendererGone

	var parts []string
	if added != 0 {
		parts = append(parts, describeDelta(added, "added"))
	}
	if removed != 0 {
		parts = append(parts, describeDelta(removed, "removed"))
	}
	if len(parts) == 0 {
		// Same counts, different content: a line was replaced rather than
		// added or removed, which counts cannot see.
		return "rewritten"
	}
	return strings.Join(parts, ", ")
}

// describeDelta words a change in line counts so it reads as English in both
// directions. "-3 added" is not a sentence; "3 fewer added" is.
func describeDelta(n int, what string) string {
	if n < 0 {
		return strconv.Itoa(-n) + " fewer " + what
	}
	return strconv.Itoa(n) + " more " + what
}

// countsFor totals a path's added and removed lines across every entry for it.
//
// A path can appear twice, once staged and once not. Taking the first match
// compared one half against itself and reported "rewritten" when a line had
// plainly been added to the other.
func (m Model) countsFor(files []fileItem, path string) (added, removed int) {
	for _, f := range files {
		if f.change.Path == path {
			added += f.change.AddedLines
			removed += f.change.DeletedLines
		}
	}
	return added, removed
}

// currentFileMoved reports whether the file on screen has different content
// from the diff being shown.
func (m Model) currentFileMoved(keys map[string]string) bool {
	if m.rendererKey == "" {
		return false
	}
	now, known := keys[m.rendererPath]
	if !known {
		// Appearing or vanishing is a change to the file list, which is
		// refreshed either way.
		return false
	}
	return now != m.rendererKey
}

// fileKeyOf is one path's content key, for a caller that has just read that
// path's diff and wants to record what it read.
func fileKeyOf(repo *git.Repo, f fileItem, stagedOnly bool) string {
	keys := fileKeysOf(repo, []fileItem{f}, stagedOnly)
	return keys[f.change.Path]
}

// followCursorTo moves the cursor onto the entry for path, preferring the half
// — staged or unstaged — it was on before the list was replaced.
//
// git reports a file with both staged and unstaged changes twice. Matching on
// the path alone put the cursor on whichever came first, which is a different
// diff: loadDiffCmd reads Staged from the entry, so the next reload would swap
// content the reviewer never asked to see.
func (m Model) followCursorTo(path string, staged bool) Model {
	if path == "" {
		return m
	}
	fallback := -1
	for i, f := range m.files {
		if f.change.Path != path {
			continue
		}
		if f.change.Staged == staged {
			m.cursor = i
			return m
		}
		if fallback < 0 {
			fallback = i
		}
	}
	if fallback >= 0 {
		m.cursor = fallback
	}
	return m
}
