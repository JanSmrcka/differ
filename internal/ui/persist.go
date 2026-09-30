package ui

import (
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

// openReviewStore finds where this checkout keeps its review and reads back
// whatever was left there. Both results are nil when there is nowhere to keep
// it, which is a review that works and does not survive, not a failure worth
// saying anything about.
func openReviewStore(repo *git.Repo) (*review.Store, *review.Session) {
	if repo == nil {
		return nil, nil
	}
	gitDir, err := repo.GitDir()
	if err != nil {
		return nil, nil
	}
	store := review.NewStore(gitDir)
	return store, store.Load(freshKeys(repo))
}

// It goes through fileKeysOf, the same function the change detector uses, so
// "the file moved" cannot come to mean two different things — and so staging,
// or a formatter writing the same bytes back, is not mistaken for a rewrite
// here either.
// freshKeys fingerprints files on disk, whatever mode differ is in.
//
// Deliberately not the mode-dependent key that change detection uses. That one
// answers "is the diff on screen out of date", which depends on what is being
// diffed — the index under -s, a ref under -r. Persistence asks a different
// question: has the file the reviewer read been re-saved since? That is the
// working tree in every mode.
//
// Using the change-detection key here meant a review written with `differ` and
// reopened with `differ -s` found no comparable key, dropped every pending
// comment, and then overwrote the file on the next change — losing them for
// good rather than merely not showing them.
func freshKeys(repo *git.Repo) review.Keys {
	return func(paths []string) map[string]string {
		if len(paths) == 0 {
			return nil
		}
		keys := make(map[string]string, len(paths))
		for _, p := range paths {
			keys[p] = worktreeKey(repo, p)
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
	if err := m.store.Save(m.session, m.reviewKeys()); err != nil {
		return m.fail("saving the review", err)
	}
	return m
}

// reviewKeys answers with what the last refresh measured, and only reads a
// file it has no answer for.
//
// Preferring the stored fingerprints is not just about the reads. They are the
// content differ has actually shown the reviewer; a fresh read at save time
// would record whatever the agent had written a moment earlier, and the
// comment would come back on the next start attached to a version of the file
// its author never saw.
func (m Model) reviewKeys() review.Keys {
	fresh := freshKeys(m.repo)
	return func(paths []string) map[string]string {
		out := make(map[string]string, len(paths))
		var unknown []string
		for _, p := range paths {
			// m.fileKeys is only comparable when it is measuring the same
			// thing: under -s and -r it holds index oids and ref hashes, which
			// a later run in another mode cannot match.
			if key, ok := m.fileKeys[p]; ok && !m.stagedOnly && m.ref == "" {
				out[p] = key
				continue
			}
			unknown = append(unknown, p)
		}
		for p, key := range fresh(unknown) {
			out[p] = key
		}
		return out
	}
}
