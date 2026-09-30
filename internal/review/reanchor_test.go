package review

import "testing"

// lines builds the "where each line lives now" view the UI passes in.
func lines(ls ...Location) []Location { return ls }

func newLine(n int, content string) Location {
	return Location{Side: SideNew, Line: n, Content: content}
}

func oldLine(n int, content string) Location {
	return Location{Side: SideOld, Line: n, Content: content}
}

func seed(t *testing.T, c Comment) (*Session, Comment) {
	t.Helper()
	s := NewSession()
	return s, s.Add(c)
}

func stateOf(t *testing.T, s *Session, id string) Comment {
	t.Helper()
	c, ok := s.Get(id)
	if !ok {
		t.Fatalf("comment %s disappeared", id)
	}
	return c
}

// An edit elsewhere in the file shifts the line number; the comment must
// follow its content rather than stay on a stale number.
func TestReanchor_FollowsContentWhenLinesShift(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideNew, StartLine: 10, EndLine: 10,
		Anchor: "  return user", Body: "note"})

	// Two lines were inserted above, so the anchor is now at 12.
	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(9, "other"), newLine(12, "  return user")), Locate: LocateLine})

	got := stateOf(t, s, c.ID)
	if got.State != StatePending {
		t.Errorf("State = %v, want pending — the anchor still exists", got.State)
	}
	if got.StartLine != 12 || got.EndLine != 12 {
		t.Errorf("lines = %d-%d, want 12-12", got.StartLine, got.EndLine)
	}
}

func TestReanchor_UnchangedDiffLeavesEverythingAlone(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideNew, StartLine: 10, EndLine: 10,
		Anchor: "  return user", Body: "note"})

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(10, "  return user")), Locate: LocateLine})

	got := stateOf(t, s, c.ID)
	if got.State != StatePending || got.StartLine != 10 {
		t.Errorf("got state=%v line=%d, want pending at 10", got.State, got.StartLine)
	}
}

// The anchored line itself changed: the comment refers to code that is gone.
func TestReanchor_ModifiedAnchorGoesStale(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideNew, StartLine: 10, EndLine: 10,
		Anchor: "  return user", Body: "note"})

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(10, "  return await user")), Locate: LocateLine})

	if got := stateOf(t, s, c.ID); got.State != StateStale {
		t.Errorf("State = %v, want stale", got.State)
	}
}

func TestReanchor_DeletedAnchorGoesStale(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideNew, StartLine: 10, EndLine: 10,
		Anchor: "  return user", Body: "note"})

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(10, "something else")), Locate: LocateLine})

	if got := stateOf(t, s, c.ID); got.State != StateStale {
		t.Errorf("State = %v, want stale", got.State)
	}
}

// A comment must never be moved onto different content.
func TestReanchor_NeverAttachesToDifferentContent(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideNew, StartLine: 10, EndLine: 10,
		Anchor: "  return user", Body: "note"})

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(10, "totally different"), newLine(11, "also different")), Locate: LocateLine})

	got := stateOf(t, s, c.ID)
	if got.State != StateStale {
		t.Fatalf("State = %v, want stale", got.State)
	}
	// The recorded position must not have moved onto the wrong line.
	if got.StartLine != 10 {
		t.Errorf("stale comment was moved to %d", got.StartLine)
	}
}

// Identical lines are common ("}"). The nearest one wins, which is still the
// same content — not a silent jump to something else.
func TestReanchor_AmbiguousAnchorPicksTheNearest(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideNew, StartLine: 20, EndLine: 20,
		Anchor: "}", Body: "note"})

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(4, "}"), newLine(21, "}"), newLine(60, "}")), Locate: LocateLine})

	got := stateOf(t, s, c.ID)
	if got.State != StatePending {
		t.Errorf("State = %v, want pending", got.State)
	}
	if got.StartLine != 21 {
		t.Errorf("StartLine = %d, want 21 (nearest match)", got.StartLine)
	}
}

func TestReanchor_RespectsSide(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideOld, StartLine: 5, EndLine: 5,
		Anchor: "  await persist(data)", Body: "note"})

	// The same text exists, but only on the new side.
	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(5, "  await persist(data)")), Locate: LocateLine})

	if got := stateOf(t, s, c.ID); got.State != StateStale {
		t.Errorf("State = %v, want stale — the old side no longer has that line", got.State)
	}
}

// The mirror of the test above: an old-side comment follows an old-side line.
func TestReanchor_OldSideCommentFollowsOldSideLine(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideOld, StartLine: 5, EndLine: 5,
		Anchor: "  await persist(data)", Body: "note"})

	s.Reanchor("a.ts", Anchored{Locations: lines(
		newLine(5, "  await persist(data)"), // same text, wrong side
		oldLine(8, "  await persist(data)"), // the real match
	), Locate: LocateLine})

	got := stateOf(t, s, c.ID)
	if got.State != StatePending {
		t.Fatalf("State = %v (%s), want pending", got.State, got.StaleReason)
	}
	if got.StartLine != 8 {
		t.Errorf("StartLine = %d, want 8 — it should match on the old side", got.StartLine)
	}
}

// A range comment keeps its span when it shifts.
func TestReanchor_RangeCommentShiftsWholeSpan(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideNew, StartLine: 10, EndLine: 14,
		Anchor: "func main() {", Body: "note"})

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(13, "func main() {")), Locate: LocateLine})

	got := stateOf(t, s, c.ID)
	if got.StartLine != 13 || got.EndLine != 17 {
		t.Errorf("range = %d-%d, want 13-17 (shifted by 3)", got.StartLine, got.EndLine)
	}
}

// A sent comment that shifts stays sent; it must not revert to pending.
func TestReanchor_SentCommentKeepsItsState(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideNew, StartLine: 10, EndLine: 10,
		Anchor: "  return user", Body: "note"})
	s.MarkSent([]string{c.ID})

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(12, "  return user")), Locate: LocateLine})

	if got := stateOf(t, s, c.ID); got.State != StateSent {
		t.Errorf("State = %v, want sent", got.State)
	}
}

// A transient refresh must not permanently stale a comment: if the anchor
// comes back, so does the comment.
func TestReanchor_RecoversWhenTheAnchorReappears(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideNew, StartLine: 10, EndLine: 10,
		Anchor: "  return user", Body: "note"})

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(10, "gone")), Locate: LocateLine})
	if got := stateOf(t, s, c.ID); got.State != StateStale {
		t.Fatalf("State = %v, want stale", got.State)
	}

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(11, "  return user")), Locate: LocateLine})
	got := stateOf(t, s, c.ID)
	if got.State != StatePending {
		t.Errorf("State = %v, want pending again", got.State)
	}
	if got.StartLine != 11 {
		t.Errorf("StartLine = %d, want 11", got.StartLine)
	}
}

func TestReanchor_RecoveredSentCommentReturnsToSent(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideNew, StartLine: 10, EndLine: 10,
		Anchor: "  return user", Body: "note"})
	s.MarkSent([]string{c.ID})

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(10, "gone")), Locate: LocateLine})
	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(10, "  return user")), Locate: LocateLine})

	if got := stateOf(t, s, c.ID); got.State != StateSent {
		t.Errorf("State = %v, want sent", got.State)
	}
}

func TestReanchor_OnlyTouchesTheNamedFile(t *testing.T) {
	s := NewSession()
	a := s.Add(Comment{File: "a.ts", Side: SideNew, StartLine: 1, EndLine: 1, Anchor: "x", Body: "a"})
	b := s.Add(Comment{File: "b.ts", Side: SideNew, StartLine: 1, EndLine: 1, Anchor: "y", Body: "b"})

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(1, "x")), Locate: LocateLine})

	if got := stateOf(t, s, b.ID); got.State != StatePending {
		t.Errorf("b.ts comment State = %v — Reanchor touched another file", got.State)
	}
	if got := stateOf(t, s, a.ID); got.State != StatePending {
		t.Errorf("a.ts comment State = %v", got.State)
	}
}

// A comment with no anchor text cannot be verified, so it is left alone
// rather than staled on every refresh.
func TestReanchor_EmptyAnchorIsLeftAlone(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideNew, StartLine: 3, EndLine: 3, Body: "note"})

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(1, "x")), Locate: LocateLine})

	got := stateOf(t, s, c.ID)
	if got.State != StatePending || got.StartLine != 3 {
		t.Errorf("got state=%v line=%d, want it untouched", got.State, got.StartLine)
	}
}

// #44: comments on a file that vanished from the diff must be marked, not
// silently kept as if they still applied.
func TestStaleMissingFiles(t *testing.T) {
	s := NewSession()
	kept := s.Add(Comment{File: "a.ts", Side: SideNew, StartLine: 1, EndLine: 1, Anchor: "x", Body: "keep"})
	gone := s.Add(Comment{File: "removed.ts", Side: SideNew, StartLine: 1, EndLine: 1, Anchor: "y", Body: "gone"})

	s.StaleMissingFiles([]string{"a.ts"})

	if got := stateOf(t, s, gone.ID); got.State != StateStale {
		t.Errorf("comment on a vanished file State = %v, want stale", got.State)
	}
	if got := stateOf(t, s, kept.ID); got.State != StatePending {
		t.Errorf("comment on a present file State = %v, want pending", got.State)
	}
}

func TestStaleReason_ExplainsWhy(t *testing.T) {
	s, c := seed(t, Comment{File: "a.ts", Side: SideNew, StartLine: 10, EndLine: 10,
		Anchor: "  return user", Body: "note"})

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(10, "changed")), Locate: LocateLine})

	got := stateOf(t, s, c.ID)
	if got.StaleReason == "" {
		t.Error("a stale comment should record why, so the user can decide what to do")
	}
}

func TestStaleCount(t *testing.T) {
	s := NewSession()
	s.Add(Comment{File: "a.ts", Side: SideNew, StartLine: 1, EndLine: 1, Anchor: "x", Body: "a"})
	s.Add(Comment{File: "a.ts", Side: SideNew, StartLine: 2, EndLine: 2, Anchor: "y", Body: "b"})

	s.Reanchor("a.ts", Anchored{Locations: lines(newLine(1, "x")), Locate: LocateLine})

	if got := s.StaleCount(); got != 1 {
		t.Errorf("StaleCount = %d, want 1", got)
	}
	if got := s.PendingCount(); got != 1 {
		t.Errorf("PendingCount = %d, want 1", got)
	}
}
