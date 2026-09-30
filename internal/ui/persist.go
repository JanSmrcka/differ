package ui

import (
	"errors"

	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
)

// Keeping a review across a restart.
//
// A review of a large changeset is an hour of close reading, and it used to
// end with the process: `q` pressed by accident, a closed tmux window, a
// `kill`. Confirming the quit only helps with the first of those, which is why
// the state is written when it changes rather than on the way out.
//
// The UI is the only part of this that knows how to fingerprint a file, so it
// is the UI that hands review.Store the answer to "is this file still what it
// was?". internal/review must not reach back into internal/ui — the diff
// parser lives here — and it does not need to: it asks a function.

// openReviewStore finds where this checkout keeps its review, claims it for
// this process, and reads back whatever was left there.
//
// The store is nil when there is nowhere to keep the review, or when another
// differ already has it: both are a review that works and does not survive.
// The second case gets a note for the status bar, because it is a choice the
// reviewer can act on — close the other differ, or accept that this one's
// comments end with it — and silently not saving is exactly the failure the
// whole feature exists to prevent.
func openReviewStore(repo *git.Repo) (*review.Store, *review.Session, *review.Lock, string) {
	if repo == nil {
		return nil, nil, nil, ""
	}
	gitDir, err := repo.GitDir()
	if err != nil {
		return nil, nil, nil, ""
	}
	lock, err := review.TakeLock(gitDir)
	if errors.Is(err, review.ErrHeldElsewhere) {
		return nil, nil, nil, "another differ has this review open — comments here are not saved"
	}
	// Any other failure leaves the store in place, unlocked. A .git/differ
	// that cannot be created is a broken directory, so nobody else can be
	// holding a lock in it either — and the saves that then fail report the
	// real reason on every comment, which is louder and more accurate than
	// one line at startup about a lock.
	store := review.NewStore(gitDir)
	return store, store.Load(currentKeys(repo)), lock, ""
}

// currentKeys fingerprints files in both scopes: the bytes on disk, and the
// staged content git holds for them.
//
// Both, because a comment records which one its author read — the working
// tree normally, the index under -s and in the commit review. Measuring only
// one meant a review written in one mode and reopened in another found no
// comparable key, dropped every pending comment, and then overwrote the file
// on the next change, losing them for good rather than merely not showing
// them. Measuring the *mode's* key was the same bug in a different direction:
// under -s an unstaged edit dropped comments about staged content that had
// not moved.
//
// The index side is one git call for the whole list, so asking for both costs
// one process per save rather than one per file.
func currentKeys(repo *git.Repo) review.Keys {
	return func(paths []string) map[string]review.ContentKey {
		if len(paths) == 0 || repo == nil {
			return nil
		}
		staged, err := repo.IndexHashes()
		indexReadable := err == nil
		keys := make(map[string]review.ContentKey, len(paths))
		for _, p := range paths {
			worktree := worktreeKey(repo, p)
			keys[p] = review.ContentKey{
				Worktree: worktree,
				Index:    indexKey(staged, p, indexReadable),
			}
		}
		return keys
	}
}

// persistReview writes the session out. Called after anything that changes a
// comment — never on quit, because quitting is the case that works already.
//
// A failure is reported rather than swallowed. It takes a read-only .git to
// produce one, and a reviewer who believes their comments are safe when they
// are not is worse off than one who is told the same thing on every comment.
func (m Model) persistReview() Model {
	if m.store == nil || m.session == nil {
		return m
	}
	if err := m.store.Save(m.session, currentKeys(m.repo)); err != nil {
		return m.fail("saving the review", err)
	}
	return m
}

// Close gives up this process's claim on the review file, so the next differ
// in this repository can save.
//
// Called by cmd once the program has returned, rather than on every path that
// quits: there are five of those and a sixth would not be noticed. A claim
// left behind by a kill is taken over by the next start.
func (m Model) Close() {
	m.reviewLock.Release()
}
