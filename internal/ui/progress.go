package ui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jansmrcka/differ/internal/git"
)

// Noticing that a file moved under the reviewer.
//
// The signal has to be cheap — it runs on every refresh — and it has to be
// quiet: a false "changed" un-reviews a file the user did read, and the ratio
// in the status bar goes backwards for no reason they can see.

// fileKeysOf fingerprints each changed file, one key per path.
//
// The only cost on top of the git status a refresh already runs is a stat per
// changed file, and that is skipped entirely under -s.
//
// Three things are deliberately *not* in the key:
//
//   - The staged flag. Staging changes nothing about a file's content, and
//     including it meant `git add` — or the user pressing `a` — marked every
//     file they had read as rewritten.
//   - Per-entry line counts. git reports a file with both staged and unstaged
//     changes twice, and staging moves lines from one half to the other; the
//     halves are summed so the total is what moves, not the split.
//   - The worktree file under -s. There the reviewer is looking at the index,
//     so an unstaged write is not a change to what is on screen.
//
// Under -s the key is therefore the line counts alone, which misses a staged
// edit that happens to keep them identical. That is a known gap, and the
// honest one: the alternative is hashing the staged blob on every refresh.
func fileKeysOf(repo *git.Repo, files []fileItem, stagedOnly bool) map[string]string {
	type totals struct{ added, deleted int }
	sums := map[string]totals{}
	order := make([]string, 0, len(files))
	for _, f := range files {
		c := f.change
		if _, seen := sums[c.Path]; !seen {
			order = append(order, c.Path)
		}
		t := sums[c.Path]
		t.added += c.AddedLines
		t.deleted += c.DeletedLines
		sums[c.Path] = t
	}

	keys := make(map[string]string, len(order))
	for _, path := range order {
		t := sums[path]
		key := fmt.Sprintf("%d|%d", t.added, t.deleted)
		if !stagedOnly && repo != nil {
			if st, err := os.Stat(filepath.Join(repo.Dir(), path)); err == nil {
				key += fmt.Sprintf("|%d|%d", st.Size(), st.ModTime().UnixNano())
			}
		}
		keys[path] = key
	}
	return keys
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
