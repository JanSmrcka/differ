// Package review holds the state of a human review session: comments the user
// has written against a diff, and how far through the changes they are.
//
// It deliberately knows nothing about git, lipgloss or the diff parser. A
// comment carries the text it needs (anchor and excerpt) so that generating
// feedback for a coding agent stays a pure string transformation.
//
// Session state lives for as long as the process. Nothing here is persisted.
package review

import (
	"fmt"
	"sort"
)

// State is where a comment is in its life: written, delivered, or no longer
// matching the diff it was written against.
type State int

const (
	StatePending State = iota
	StateSent
	StateStale
)

func (s State) String() string {
	switch s {
	case StateSent:
		return "sent"
	case StateStale:
		return "stale"
	default:
		return "pending"
	}
}

// Side records whether a comment is anchored to the old or the new version of
// a file, which decides how its line numbers should be read.
type Side int

const (
	SideNew Side = iota
	SideOld
)

// Comment is one piece of review feedback about a range of lines in a file.
type Comment struct {
	ID   string
	File string
	Side Side
	// StartLine and EndLine are 1-based line numbers on Side.
	StartLine int
	EndLine   int
	HunkIndex int
	// Anchor is the source text the comment was attached to. It is what lets
	// a comment be re-located, or found to be stale, after the diff changes.
	Anchor string
	// Excerpt is the diff context shown to whoever receives this feedback.
	Excerpt string
	Body    string
	State   State

	// seq is the creation order, used to break ties between comments on the
	// same line. IDs are strings ("c9", "c10"), so comparing them would order
	// the tenth comment before the ninth.
	seq int
}

// FileState is how far the user has got with one file.
type FileState int

const (
	FileUnreviewed FileState = iota
	FileViewed
	FileCommented
)

// Progress summarises a session against the files currently in the diff.
type Progress struct {
	Total    int
	Reviewed int
	Comments int
	Pending  int
	Sent     int
	Stale    int
}

// Session is the review state for one run of the application.
type Session struct {
	comments []Comment
	viewed   map[string]bool
	nextID   int
}

func NewSession() *Session {
	return &Session{viewed: map[string]bool{}}
}

// Add stores a comment, assigning it an ID, and returns the stored copy.
func (s *Session) Add(c Comment) Comment {
	s.nextID++
	c.ID = fmt.Sprintf("c%d", s.nextID)
	c.seq = s.nextID
	c.State = StatePending
	if c.EndLine < c.StartLine {
		c.EndLine = c.StartLine
	}
	s.comments = append(s.comments, c)
	return c
}

// Comments returns every comment in the session, ordered by file then line.
func (s *Session) Comments() []Comment {
	out := make([]Comment, len(s.comments))
	copy(out, s.comments)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].StartLine != out[j].StartLine {
			return out[i].StartLine < out[j].StartLine
		}
		return out[i].seq < out[j].seq
	})
	return out
}

// CommentsFor returns one file's comments, ordered by line.
func (s *Session) CommentsFor(file string) []Comment {
	var out []Comment
	for _, c := range s.comments {
		if c.File == file {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartLine < out[j].StartLine })
	return out
}

// CountFor is how many comments a file carries.
func (s *Session) CountFor(file string) int {
	n := 0
	for _, c := range s.comments {
		if c.File == file {
			n++
		}
	}
	return n
}

// Get returns a comment by ID.
func (s *Session) Get(id string) (Comment, bool) {
	for _, c := range s.comments {
		if c.ID == id {
			return c, true
		}
	}
	return Comment{}, false
}

// UpdateBody replaces a comment's text, reporting whether it existed.
func (s *Session) UpdateBody(id, body string) bool {
	for i := range s.comments {
		if s.comments[i].ID == id {
			s.comments[i].Body = body
			return true
		}
	}
	return false
}

// Remove discards a comment, reporting whether it existed.
func (s *Session) Remove(id string) bool {
	for i := range s.comments {
		if s.comments[i].ID == id {
			s.comments = append(s.comments[:i], s.comments[i+1:]...)
			return true
		}
	}
	return false
}

// MarkSent records that the named comments were delivered.
func (s *Session) MarkSent(ids []string) {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	for i := range s.comments {
		if want[s.comments[i].ID] {
			s.comments[i].State = StateSent
		}
	}
}

// MarkViewed records that the user has looked at a file.
func (s *Session) MarkViewed(file string) { s.viewed[file] = true }

// FileStateOf reports how far the user has got with a file. Carrying comments
// outranks merely having been viewed.
func (s *Session) FileStateOf(file string) FileState {
	if s.CountFor(file) > 0 {
		return FileCommented
	}
	if s.viewed[file] {
		return FileViewed
	}
	return FileUnreviewed
}

// PendingCount and SentCount count comments by state.
func (s *Session) PendingCount() int { return s.countState(StatePending) }
func (s *Session) SentCount() int    { return s.countState(StateSent) }
func (s *Session) StaleCount() int   { return s.countState(StateStale) }

func (s *Session) countState(st State) int {
	n := 0
	for _, c := range s.comments {
		if c.State == st {
			n++
		}
	}
	return n
}

// Progress summarises the session against the files currently in the diff.
// Files that are no longer part of the diff do not count towards progress.
func (s *Session) Progress(files []string) Progress {
	p := Progress{Total: len(files)}
	current := make(map[string]bool, len(files))
	for _, f := range files {
		current[f] = true
		if s.FileStateOf(f) != FileUnreviewed {
			p.Reviewed++
		}
	}
	for _, c := range s.comments {
		if !current[c.File] {
			continue
		}
		p.Comments++
		switch c.State {
		case StatePending:
			p.Pending++
		case StateSent:
			p.Sent++
		case StateStale:
			p.Stale++
		}
	}
	return p
}
