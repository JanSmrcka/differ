package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Where a review goes when the process does not survive.
//
// Comments are an hour of close reading; `q` pressed by accident, a closed
// tmux window or a `kill` used to end that hour. The file lives in the
// repository's own git directory, which is per checkout, never committed and
// needs no .gitignore entry — and which dies with the repository, so a deleted
// working tree leaves no orphan behind.

// storeVersion is the format the file is written in. A file written by any
// other version is ignored rather than migrated: this is a cache of work in
// progress, not a document, and the cost of getting a migration wrong is
// higher than the cost of losing comments once.
const storeVersion = 1

// Keys answers what each named file's content key is now.
//
// It takes the whole list at once rather than one path at a time because under
// -s a key comes from git's view of the entire index, read in one call. Asked
// file by file, a review touching a dozen files would start a dozen git
// processes every time a comment changed.
type Keys func(files []string) map[string]string

// Store is one review's state on disk.
type Store struct{ path string }

// NewStore puts the file inside gitDir, which is a repository's own git
// directory — `git rev-parse --git-dir`.
func NewStore(gitDir string) *Store {
	return &Store{path: filepath.Join(gitDir, "differ", "review.json")}
}

// Path is where the state is written, for a caller that needs to say so.
func (st *Store) Path() string { return st.path }

// The file's shape, written out explicitly rather than by marshalling the
// session's own structs. What is on disk is a format that has to be readable
// by the next version of differ; renaming a field in Comment must not silently
// invalidate every saved review.
type storedReview struct {
	Version int `json:"version"`
	// NextID continues the session's numbering. It is saved rather than
	// recomputed because the history refers to comments by id, and reusing the
	// id of a comment that was dropped would make an entry in it describe
	// something else.
	NextID   int               `json:"next_id"`
	Files    map[string]string `json:"files,omitempty"`
	Comments []storedComment   `json:"comments,omitempty"`
	History  []storedDelivery  `json:"history,omitempty"`
}

type storedComment struct {
	ID        string `json:"id"`
	File      string `json:"file"`
	Side      int    `json:"side"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	HunkIndex int    `json:"hunk_index"`
	Anchor    string `json:"anchor"`
	Excerpt   string `json:"excerpt"`
	Body      string `json:"body"`
}

type storedDelivery struct {
	At       string   `json:"at"`
	Target   string   `json:"target"`
	Comments []string `json:"comments"`
	Files    []string `json:"files"`
	Err      string   `json:"err,omitempty"`
}

// Save writes the session out.
func (st *Store) Save(s *Session, keys Keys) error {
	if s == nil {
		return nil
	}
	out := storedReview{Version: storeVersion, NextID: s.nextID}
	for _, c := range s.comments {
		// Anything already delivered is written out as history and nothing
		// else. Bringing it back as a live comment would put it in the next
		// send, and handing the agent a review it has already acted on is the
		// one failure this file must not introduce.
		if c.WasSent() {
			continue
		}
		out.Comments = append(out.Comments, storedComment{
			ID: c.ID, File: c.File, Side: int(c.Side),
			StartLine: c.StartLine, EndLine: c.EndLine, HunkIndex: c.HunkIndex,
			Anchor: c.Anchor, Excerpt: c.Excerpt, Body: c.Body,
		})
	}
	if keys != nil {
		out.Files = keys(filesOf(out.Comments))
	}
	for _, d := range s.deliveries {
		out.History = append(out.History, storedDelivery{
			At:     d.At.Format(time.RFC3339Nano),
			Target: d.Target, Comments: d.Comments, Files: d.Files, Err: d.Err,
		})
	}

	// Indented, like the config file: it is small, it is written when a
	// person does something rather than in a loop, and the one time anybody
	// reads it by hand is when something has gone wrong with it.
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(st.path, data)
}

// Load reads the session back, or nil when there is nothing to restore.
func (st *Store) Load(keys Keys) *Session {
	data, err := os.ReadFile(st.path)
	if err != nil {
		return nil
	}
	var in storedReview
	if err := json.Unmarshal(data, &in); err != nil || in.Version != storeVersion {
		return nil
	}

	s := NewSession()
	s.nextID = in.NextID
	s.comments = restoredComments(in, keys)
	// The history comes back whatever happened to the files. It answers "what
	// have I already told the agent?", and that question does not stop being
	// asked because the code has moved on — it is asked *because* it has.
	for _, sd := range in.History {
		s.deliveries = append(s.deliveries, Delivery{
			At:     parseTime(sd.At),
			Target: sd.Target, Comments: sd.Comments, Files: sd.Files, Err: sd.Err,
		})
	}
	if len(s.comments) == 0 && len(s.deliveries) == 0 {
		// Nothing came back — every comment was deleted, or every one of them
		// belonged to a file that has been rewritten. The caller gets the
		// same answer as if nothing had ever been written, because that is
		// the same situation.
		return nil
	}
	return s
}

// restoredComments is the saved comments whose file is still what it was.
//
// A comment is work about particular code. If the agent replaced that code
// while differ was not running, the comment is worse than nothing, and the
// file goes back to unreviewed because it has to be read again — which takes
// no more than dropping the comments, since nothing else about a file is
// saved. A file with no answer at all counts as rewritten: failing closed is
// the whole value of the check.
//
// Everything comes back pending. State is not saved because it is not a fact
// about the comment — it is what the diff currently says about it, and the
// caller re-anchors what it gets, which is what decides it again.
func restoredComments(in storedReview, keys Keys) []Comment {
	now := map[string]string{}
	if keys != nil {
		now = keys(filesOf(in.Comments))
	}

	var out []Comment
	for _, sc := range in.Comments {
		if was, ok := in.Files[sc.File]; !ok || was == "" || now[sc.File] != was {
			continue
		}
		out = append(out, Comment{
			ID: sc.ID, File: sc.File, Side: Side(sc.Side),
			StartLine: sc.StartLine, EndLine: sc.EndLine, HunkIndex: sc.HunkIndex,
			Anchor: sc.Anchor, Excerpt: sc.Excerpt, Body: sc.Body,
			State: StatePending, seq: len(out) + 1,
		})
	}
	return out
}

// parseTime reads a stored timestamp, falling back to the zero time.
//
// The timestamp is kept as a string, and parsed here rather than by the JSON
// decoder, so that one unreadable clock costs that entry its time of day
// instead of costing the whole file: a time.Time field would fail the decode
// of the entire document, and the record of what was sent is worth more than
// the hour it was sent at.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// filesOf names the files a set of stored comments came from, each once.
func filesOf(cs []storedComment) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range cs {
		if seen[c.File] {
			continue
		}
		seen[c.File] = true
		out = append(out, c.File)
	}
	return out
}

// writeFileAtomically replaces a file in one step.
//
// A half-written review.json is worse than none: the next start would read a
// truncated file, and the comments it did contain would be unreachable either
// way. Writing a temporary file in the same directory and renaming it over the
// target means a reader sees the old file or the new one, never a partial one.
func writeFileAtomically(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".review-*.json")
	if err != nil {
		return err
	}
	// Named now, because the cleanup below has to work after tmp is closed.
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
