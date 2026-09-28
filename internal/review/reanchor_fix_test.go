package review

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// A sent comment must never be re-delivered, even after its file leaves the
// diff and it is marked stale.
func TestWasSent_SurvivesGoingStale(t *testing.T) {
	s := NewSession()
	c := s.Add(Comment{File: "a.ts", Side: SideNew, StartLine: 1, EndLine: 1, Anchor: "x", Body: "note"})
	s.MarkSent([]string{c.ID})

	s.StaleMissingFiles([]string{"other.ts"})

	got, _ := s.Get(c.ID)
	if got.State != StateStale {
		t.Fatalf("State = %v, want stale once the file left the diff", got.State)
	}
	if !got.WasSent() {
		t.Error("a comment that was delivered must still report WasSent, or it can be sent twice")
	}
}

func TestWasSent_PendingCommentIsNot(t *testing.T) {
	s := NewSession()
	c := s.Add(Comment{File: "a.ts", Side: SideNew, StartLine: 1, EndLine: 1, Anchor: "x", Body: "note"})
	if got, _ := s.Get(c.ID); got.WasSent() {
		t.Error("a pending comment must not report WasSent")
	}
}

// A comment must not hop to a distant block that happens to share its text.
func TestReanchor_DoesNotHopToADistantIdenticalLine(t *testing.T) {
	s := NewSession()
	c := s.Add(Comment{File: "a.ts", Side: SideNew, StartLine: 10, EndLine: 10, Anchor: "}", Body: "note"})

	// The block at line 10 is gone; the only other "}" is far away.
	s.Reanchor("a.ts", lines(newLine(400, "}")))

	got, _ := s.Get(c.ID)
	if got.State != StateStale {
		t.Errorf("State = %v, want stale — line 400 is a different block", got.State)
	}
	if got.StartLine != 10 {
		t.Errorf("StartLine = %d, want it left at 10", got.StartLine)
	}
}

// Two comments on identical text must not collapse onto the same line.
func TestReanchor_TwoCommentsDoNotClaimTheSameLine(t *testing.T) {
	s := NewSession()
	a := s.Add(Comment{File: "a.ts", Side: SideNew, StartLine: 10, EndLine: 10, Anchor: "}", Body: "first"})
	b := s.Add(Comment{File: "a.ts", Side: SideNew, StartLine: 12, EndLine: 12, Anchor: "}", Body: "second"})

	// Only one "}" survives, near both.
	s.Reanchor("a.ts", lines(newLine(11, "}")))

	ga, _ := s.Get(a.ID)
	gb, _ := s.Get(b.ID)
	if ga.State == StatePending && gb.State == StatePending && ga.StartLine == gb.StartLine {
		t.Errorf("both comments claim line %d — one of them is pointing at code it was not written about", ga.StartLine)
	}
}

// A close shift is still followed, so ordinary edits do not stale a review.
func TestReanchor_NearbyShiftIsStillFollowed(t *testing.T) {
	s := NewSession()
	c := s.Add(Comment{File: "a.ts", Side: SideNew, StartLine: 10, EndLine: 10, Anchor: "}", Body: "note"})

	s.Reanchor("a.ts", lines(newLine(13, "}")))

	got, _ := s.Get(c.ID)
	if got.State != StatePending {
		t.Errorf("State = %v (%s), want pending", got.State, got.StaleReason)
	}
	if got.StartLine != 13 {
		t.Errorf("StartLine = %d, want 13", got.StartLine)
	}
}

func TestStaleReason_IsValidUTF8ForMultibyteAnchors(t *testing.T) {
	s := NewSession()
	anchor := "  const zpráva = \"příliš žluťoučký kůň úpěl ďábelské ódy a běžel přes pole\""
	c := s.Add(Comment{File: "a.ts", Side: SideNew, StartLine: 1, EndLine: 1, Anchor: anchor, Body: "note"})

	s.Reanchor("a.ts", lines(newLine(1, "changed")))

	got, _ := s.Get(c.ID)
	if !utf8.ValidString(got.StaleReason) {
		t.Errorf("stale reason is not valid UTF-8: %q", got.StaleReason)
	}
	if strings.Contains(got.StaleReason, "�") {
		t.Errorf("stale reason contains a replacement character: %q", got.StaleReason)
	}
}
