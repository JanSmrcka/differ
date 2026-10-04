// Package review holds the state of a human review session: comments the user
// has written against a diff, and how far through the changes they are.
//
// It deliberately knows nothing about git, lipgloss or the diff parser. A
// comment carries the text it needs (anchor and excerpt) so that generating
// feedback for a coding agent stays a pure string transformation.
//
// A Session itself holds nothing but memory. What survives a restart, and how
// it is decided, is store.go's business — the session is not aware of it.
package review

import (
	"fmt"
	"slices"
	"sort"
	"time"
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
// Locate says how precisely a comment can point at the file as it is now.
//
// The question is not which side of the diff the comment is on, which was the
// first answer and the wrong one. It is whether the line number resolves
// against the file on disk:
//
//   - Under -s and `differ commit` the diff's *new* side is the index, not the
//     working tree. Staging a change and then editing above it puts the code
//     eight lines down while the diff still says three, so a new-side
//     reference lands on unrelated code — the very failure that dropping
//     old-side references was meant to avoid.
//   - An old-side line number describes the file before the change, so it
//     never resolves. But the file itself usually still exists, and
//     sidekick.nvim emits a bare "@path" with no location for exactly that.
//     Attaching the file and losing the line beats attaching nothing.
//   - A file that has been deleted, or has left the changeset, has nothing to
//     point at.
type Locate int

const (
	// LocateUnknown is the zero value, and deliberately not LocateLine.
	//
	// A Comment built without setting this — a fixture, or one decoded from a
	// stored review written before the field existed — would otherwise claim
	// its line resolves, which is the one thing that must not be assumed. It
	// degrades to the file, so an unset value costs a line number rather than
	// pointing at the wrong one.
	LocateUnknown Locate = iota
	// LocateLine: the path and the line both address the file on disk.
	LocateLine
	// LocateFile: the file is there, the line numbers are not the worktree's.
	LocateFile
	// LocateNone: there is nothing on disk to point at.
	LocateNone
)

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
	// FileKey fingerprints the content this comment was written about, and
	// Scope says which content that was — the working tree, or the index.
	//
	// Recorded when the comment is written, not when the review is saved. A
	// key measured at save time is whatever the agent had written a moment
	// earlier, so a comment would come back attached to a version of the file
	// its author never read — which is the one thing the fingerprint exists
	// to prevent. The scope travels with it because differ can be reopened in
	// another mode: under -s the reviewer reads the index, and an unstaged
	// edit does not touch what they read.
	FileKey string
	Scope   KeyScope
	State   State
	// Agent is what the agent has done with the comment since it was sent,
	// when the target can say: the latest delivery it went out in, as
	// NoteAgent recorded it. Empty means nothing is known.
	Agent string

	// StaleReason says why a comment no longer matches the diff, so the user
	// can judge whether to re-create or discard it.
	StaleReason string
	// Locate says how precisely the agent can be pointed at this comment.
	// Set where the comment is built, because only there is it known whether
	// the line numbers address the working tree.
	Locate Locate

	// seq is the creation order, used to break ties between comments on the
	// same line. IDs are strings ("c9", "c10"), so comparing them would order
	// the tenth comment before the ninth.
	seq int
	// stateBeforeStale remembers what a comment was before going stale, so a
	// transient refresh does not permanently demote it.
	stateBeforeStale State
}

// WasSent reports whether this comment has already been delivered, including
// after it later went stale. Nothing that was sent may be sent again.
func (c Comment) WasSent() bool {
	return c.State == StateSent || (c.State == StateStale && c.stateBeforeStale == StateSent)
}

// FileState is how far the user has got with one file.
//
// The values are a precedence order read from the bottom up: FileStateOf
// returns the highest that applies, because that is the one the user needs to
// act on. A file that changed under them outranks anything they had already
// done with it.
type FileState int

const (
	FileUnreviewed FileState = iota
	FileViewed
	// FileSent means every comment on this file has been delivered — there is
	// nothing left to do with it.
	FileSent
	// FileCommented means comments are waiting to go out.
	FileCommented
	// FileChanged means the file changed after the user last looked at it, so
	// whatever they concluded may no longer hold.
	FileChanged
)

func (f FileState) String() string {
	switch f {
	case FileViewed:
		return "viewed"
	case FileSent:
		return "sent"
	case FileCommented:
		return "commented"
	case FileChanged:
		return "changed"
	default:
		return "unreviewed"
	}
}

// Progress summarises a session against the files currently in the diff.
type Progress struct {
	Total    int
	Reviewed int
	Comments int
	Pending  int
	Sent     int
	Stale    int
	// Changed counts files rewritten since the user last read them. They do
	// not count towards Reviewed: whatever was concluded about them was
	// concluded about different code.
	Changed int
}

// Session is the review state for one run of the application.
type Session struct {
	comments []Comment
	viewed   map[string]bool
	// changed names the files that were rewritten after the user last looked
	// at them. Looking again clears the flag.
	changed map[string]bool
	// deliveries is what left the session, in the order it was attempted,
	// failures included.
	deliveries []Delivery
	nextID     int
}

func NewSession() *Session {
	return &Session{viewed: map[string]bool{}, changed: map[string]bool{}}
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

// MarkViewed records that the user has looked at a file. It is also how a
// change is acknowledged: having looked at the new content, they are no longer
// working from the old.
func (s *Session) MarkViewed(file string) {
	s.viewed[file] = true
	delete(s.changed, file)
}

// NoteChange records that a file's content moved underneath the user.
//
// A file they have not looked at cannot go stale on them — there is nothing to
// invalidate — so the flag is only set for one they have seen.
func (s *Session) NoteChange(file string) {
	if s.viewed[file] {
		s.changed[file] = true
	}
}

// ChangedSinceViewed reports whether a file was rewritten after the user last
// read it.
func (s *Session) ChangedSinceViewed(file string) bool { return s.changed[file] }

// FileStateOf reports how far the user has got with a file, as the highest
// state that applies.
func (s *Session) FileStateOf(file string) FileState {
	if s.changed[file] {
		return FileChanged
	}
	if s.CountFor(file) > 0 {
		if s.allSent(file) {
			return FileSent
		}
		return FileCommented
	}
	if s.viewed[file] {
		return FileViewed
	}
	return FileUnreviewed
}

// allSent reports whether every comment on a file has been delivered. A file
// with no comments is not "all sent" — it has nothing to send.
func (s *Session) allSent(file string) bool {
	found := false
	for _, c := range s.comments {
		if c.File != file {
			continue
		}
		found = true
		if !c.WasSent() {
			return false
		}
	}
	return found
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
		switch s.FileStateOf(f) {
		case FileUnreviewed:
		case FileChanged:
			p.Changed++
		default:
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

// Delivery is one attempt to send feedback out of the session — the history
// the user can ask for when they lose track of what already went to the agent.
//
// The clock is the caller's: a Session has no time source of its own, so its
// tests stay deterministic.
type Delivery struct {
	At time.Time
	// Target is the delivery mechanism's name, as feedback.Target reports it.
	Target string
	// Comments are the IDs in this payload, and Files the files they came
	// from.
	Comments []string
	Files    []string
	// Err is why the delivery failed, empty when it succeeded.
	Err string
	// Agent is what the agent has done with it since, when the target can
	// say — herdr can, tmux cannot: "working", then "idle", "done" or
	// "blocked". Empty means nothing is known.
	Agent string
}

// OK reports whether the delivery succeeded.
func (d Delivery) OK() bool { return d.Err == "" }

// RecordDelivery appends an attempt to the history. Failures are recorded too:
// "did that actually go out?" is the question the history exists to answer,
// and a send that silently failed is the worst answer to be missing.
//
// It returns the delivery's index, which NoteAgent takes.
func (s *Session) RecordDelivery(d Delivery) int {
	s.deliveries = append(s.deliveries, d)
	return len(s.deliveries) - 1
}

// NoteAgent records what the agent has done with delivery i. An index that
// names no delivery is ignored: the answer arrives long after the send.
func (s *Session) NoteAgent(i int, state string) {
	if i < 0 || i >= len(s.deliveries) {
		return
	}
	s.deliveries[i].Agent = state
	for _, id := range s.deliveries[i].Comments {
		for j := range s.comments {
			if s.comments[j].ID == id {
				s.comments[j].Agent = s.AgentStateOf(id)
			}
		}
	}
}

// AgentStateOf is what the agent has done with a comment: its state on the
// latest successful delivery the comment was in, or "".
func (s *Session) AgentStateOf(id string) string {
	for i := len(s.deliveries) - 1; i >= 0; i-- {
		d := s.deliveries[i]
		if d.OK() && slices.Contains(d.Comments, id) {
			return d.Agent
		}
	}
	return ""
}

// History is every delivery attempt, most recent first.
func (s *Session) History() []Delivery {
	out := make([]Delivery, 0, len(s.deliveries))
	for i := len(s.deliveries) - 1; i >= 0; i-- {
		out = append(out, s.deliveries[i])
	}
	return out
}
