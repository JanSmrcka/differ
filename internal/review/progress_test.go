package review

import (
	"testing"
	"time"
)

// Review progress: what the user has looked at, what they have sent, and what
// the agent changed underneath them while they were reading.
//
// The status bar's progress readout is built from these states, and the
// changed-file list will show them per file (#52), so a wrong answer here
// shows up as a wrong count in front of the reviewer.

// A file whose every comment has gone out is done with, not merely commented.
func TestSession_AFileWithEverythingSentReportsSent(t *testing.T) {
	t.Parallel()
	s := NewSession()
	a := s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "one"})
	b := s.Add(Comment{File: "a.ts", StartLine: 2, EndLine: 2, Body: "two"})

	s.MarkSent([]string{a.ID})
	if got := s.FileStateOf("a.ts"); got != FileCommented {
		t.Errorf("one of two sent: state = %v, want commented", got)
	}

	s.MarkSent([]string{b.ID})
	if got := s.FileStateOf("a.ts"); got != FileSent {
		t.Errorf("both sent: state = %v, want sent", got)
	}
}

// The agent rewriting a file the user has already read is the case review mode
// exists for. Until they look again, what they concluded about it is suspect.
func TestSession_AFileThatChangedAfterBeingViewedIsMarkedChanged(t *testing.T) {
	t.Parallel()
	s := NewSession()

	// Nothing has been looked at yet, so there is nothing to invalidate.
	s.NoteChange("a.ts")
	if got := s.FileStateOf("a.ts"); got != FileUnreviewed {
		t.Errorf("unviewed file after a change: state = %v, want unreviewed", got)
	}

	s.MarkViewed("a.ts")
	s.NoteChange("a.ts")
	if got := s.FileStateOf("a.ts"); got != FileChanged {
		t.Errorf("viewed file after a change: state = %v, want changed", got)
	}

	// Looking again is how the user acknowledges it.
	s.MarkViewed("a.ts")
	if got := s.FileStateOf("a.ts"); got != FileViewed {
		t.Errorf("after looking again: state = %v, want viewed", got)
	}
}

// A commented file that changed is still worth flagging — more so, because the
// comments may now describe code that is gone.
func TestSession_ChangedOutranksComments(t *testing.T) {
	t.Parallel()
	s := NewSession()
	s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "x"})
	s.MarkViewed("a.ts")
	s.NoteChange("a.ts")

	if got := s.FileStateOf("a.ts"); got != FileChanged {
		t.Errorf("state = %v, want changed", got)
	}
}

// "Did that actually go out?" is the question the history answers, so a
// failure has to be in it as plainly as a success.
func TestSession_HistoryRecordsSuccessesAndFailures(t *testing.T) {
	t.Parallel()
	s := NewSession()
	first := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)

	s.RecordDelivery(Delivery{At: first, Target: "clipboard", Comments: []string{"c1"}, Files: []string{"a.ts"}})
	s.RecordDelivery(Delivery{At: first.Add(time.Minute), Target: "tmux", Comments: []string{"c2"}, Files: []string{"b.ts"}, Err: "no such pane"})

	got := s.History()
	if len(got) != 2 {
		t.Fatalf("history has %d entries, want 2", len(got))
	}
	// Most recent first: what just happened is what the user is asking about.
	if got[0].Target != "tmux" {
		t.Errorf("history[0].Target = %q, want the most recent (tmux)", got[0].Target)
	}
	if got[0].OK() {
		t.Error("a delivery that failed reports OK")
	}
	if !got[1].OK() {
		t.Error("a delivery that succeeded reports not OK")
	}
}

// Progress is the number in the status bar. A file the agent rewrote is not
// reviewed any more, however carefully it was read before.
func TestSession_ProgressCountsAChangedFileAsUnreviewed(t *testing.T) {
	t.Parallel()
	s := NewSession()
	files := []string{"a.ts", "b.ts"}
	s.MarkViewed("a.ts")
	s.MarkViewed("b.ts")

	if p := s.Progress(files); p.Reviewed != 2 || p.Changed != 0 {
		t.Fatalf("both viewed: Reviewed = %d, Changed = %d, want 2 and 0", p.Reviewed, p.Changed)
	}

	s.NoteChange("b.ts")
	p := s.Progress(files)
	if p.Reviewed != 1 {
		t.Errorf("Reviewed = %d, want 1 — b.ts changed", p.Reviewed)
	}
	if p.Changed != 1 {
		t.Errorf("Changed = %d, want 1", p.Changed)
	}
}
