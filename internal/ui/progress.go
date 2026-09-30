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
	if stagedOnly && repo != nil {
		// One call for the whole index, not one per file.
		staged, _ = repo.IndexHashes()
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
			keys[path] = "index:" + staged[path]
			continue
		}
		keys[path] = worktreeKey(repo, path)
	}
	return keys
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

// holdsTheDiff reports whether the content on screen belongs to someone who
// would rather be asked before it changes.
//
// Reviewing is close reading with comments attached to particular lines, so a
// silent swap can leave a pending comment describing something that is no
// longer there. Outside review mode differ stays live, which is the point of
// the poll.
func (m Model) holdsTheDiff() bool {
	// The renderer has to be the cursor's file, not just any file. Diffs load
	// asynchronously, so right after n or p the renderer is still the previous
	// one — and a refresh landing in that window would compare the cursor's
	// file against a diff of something else. Nothing needs holding there
	// anyway: the load already on its way brings current content.
	return m.mode == modeReview && m.renderer != nil && m.rendererPath == m.currentFilePath()
}

// currentFileMoved reports whether the file under the cursor has different
// content from the one the diff on screen was built from.
func (m Model) currentFileMoved(keys map[string]string) bool {
	path := m.currentFilePath()
	if path == "" {
		return false
	}
	was, knew := m.fileKeys[path]
	now, know := keys[path]
	if !knew || !know {
		// One side has never seen it. Appearing or vanishing is a change to
		// the file list rather than to the diff on screen, and the list is
		// refreshed either way.
		return false
	}
	return was != now
}

// changeSince describes how the file under the cursor differs from the diff on
// screen, for the notice.
//
// The issue asks the reload to come with a summary of what changed. Counting
// hunks would mean parsing the new diff, which is the work the hold exists to
// postpone; the added and deleted line counts are already in the refresh, so
// the difference between them is free.
func (m Model) changeSince(files []fileItem) string {
	path := m.currentFilePath()
	was, found := m.fileAt(m.files, path)
	now, stillThere := m.fileAt(files, path)
	if !found || !stillThere {
		return ""
	}
	added := now.change.AddedLines - was.change.AddedLines
	removed := now.change.DeletedLines - was.change.DeletedLines

	var parts []string
	if added != 0 {
		parts = append(parts, signed(added)+" added")
	}
	if removed != 0 {
		parts = append(parts, signed(removed)+" removed")
	}
	if len(parts) == 0 {
		// Same counts, different content: a line was replaced rather than
		// added or removed, which the counts cannot see.
		return "rewritten"
	}
	return strings.Join(parts, ", ")
}

func (m Model) fileAt(files []fileItem, path string) (fileItem, bool) {
	for _, f := range files {
		if f.change.Path == path {
			return f, true
		}
	}
	return fileItem{}, false
}

func signed(n int) string {
	if n > 0 {
		return "+" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// clearStaleNotice drops the notice, which describes one diff on screen.
func (m Model) clearStaleNotice() Model {
	m.diffStale = false
	m.staleSummary = ""
	m.stalePath = ""
	return m
}
