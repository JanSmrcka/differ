package review

import "testing"

func TestSession_AddAssignsStableIDs(t *testing.T) {
	s := NewSession()

	a := s.Add(Comment{File: "a.ts", StartLine: 10, EndLine: 10, Body: "first"})
	b := s.Add(Comment{File: "a.ts", StartLine: 20, EndLine: 20, Body: "second"})

	if a.ID == "" || b.ID == "" {
		t.Fatal("comments must get an ID")
	}
	if a.ID == b.ID {
		t.Errorf("IDs must be unique, both were %q", a.ID)
	}
	if a.State != StatePending {
		t.Errorf("new comment State = %v, want pending", a.State)
	}
}

func TestSession_CommentsForFileAreOrderedByLine(t *testing.T) {
	s := NewSession()
	s.Add(Comment{File: "a.ts", StartLine: 30, EndLine: 30, Body: "third"})
	s.Add(Comment{File: "b.ts", StartLine: 1, EndLine: 1, Body: "other file"})
	s.Add(Comment{File: "a.ts", StartLine: 10, EndLine: 10, Body: "first"})

	got := s.CommentsFor("a.ts")
	if len(got) != 2 {
		t.Fatalf("got %d comments for a.ts, want 2", len(got))
	}
	if got[0].StartLine != 10 || got[1].StartLine != 30 {
		t.Errorf("comments out of order: %d then %d", got[0].StartLine, got[1].StartLine)
	}
}

func TestSession_CountFor(t *testing.T) {
	s := NewSession()
	s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "x"})
	s.Add(Comment{File: "a.ts", StartLine: 2, EndLine: 2, Body: "y"})

	if got := s.CountFor("a.ts"); got != 2 {
		t.Errorf("CountFor(a.ts) = %d, want 2", got)
	}
	if got := s.CountFor("missing.ts"); got != 0 {
		t.Errorf("CountFor(missing.ts) = %d, want 0", got)
	}
}

func TestSession_UpdateAndRemove(t *testing.T) {
	s := NewSession()
	c := s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "before"})

	if !s.UpdateBody(c.ID, "after") {
		t.Fatal("UpdateBody reported failure for an existing comment")
	}
	if got := s.CommentsFor("a.ts")[0].Body; got != "after" {
		t.Errorf("Body = %q, want %q", got, "after")
	}
	if s.UpdateBody("nope", "x") {
		t.Error("UpdateBody should fail for an unknown ID")
	}

	if !s.Remove(c.ID) {
		t.Fatal("Remove reported failure for an existing comment")
	}
	if got := s.CountFor("a.ts"); got != 0 {
		t.Errorf("after Remove, CountFor = %d, want 0", got)
	}
	if s.Remove(c.ID) {
		t.Error("Remove should fail the second time")
	}
}

func TestSession_FileStateTracksWhatWasVisited(t *testing.T) {
	s := NewSession()

	if got := s.FileStateOf("a.ts"); got != FileUnreviewed {
		t.Errorf("unknown file state = %v, want unreviewed", got)
	}

	s.MarkViewed("a.ts")
	if got := s.FileStateOf("a.ts"); got != FileViewed {
		t.Errorf("after MarkViewed state = %v, want viewed", got)
	}

	// A file carrying comments reads as commented, which outranks "viewed".
	s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "x"})
	if got := s.FileStateOf("a.ts"); got != FileCommented {
		t.Errorf("file with comments state = %v, want commented", got)
	}
}

func TestSession_MarkViewedDoesNotDowngradeACommentedFile(t *testing.T) {
	s := NewSession()
	s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "x"})
	s.MarkViewed("a.ts")

	if got := s.FileStateOf("a.ts"); got != FileCommented {
		t.Errorf("state = %v, want commented", got)
	}
}

func TestSession_PendingCount(t *testing.T) {
	s := NewSession()
	s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "x"})
	c := s.Add(Comment{File: "a.ts", StartLine: 2, EndLine: 2, Body: "y"})
	s.MarkSent([]string{c.ID})

	if got := s.PendingCount(); got != 1 {
		t.Errorf("PendingCount = %d, want 1", got)
	}
	if got := s.SentCount(); got != 1 {
		t.Errorf("SentCount = %d, want 1", got)
	}
}

func TestSession_MarkSentOnlyAffectsNamedComments(t *testing.T) {
	s := NewSession()
	a := s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "x"})
	s.Add(Comment{File: "a.ts", StartLine: 2, EndLine: 2, Body: "y"})

	s.MarkSent([]string{a.ID})

	for _, c := range s.CommentsFor("a.ts") {
		want := StatePending
		if c.ID == a.ID {
			want = StateSent
		}
		if c.State != want {
			t.Errorf("comment %s State = %v, want %v", c.ID, c.State, want)
		}
	}
}

func TestSession_ReviewedFilesProgress(t *testing.T) {
	s := NewSession()
	s.MarkViewed("a.ts")
	s.Add(Comment{File: "b.ts", StartLine: 1, EndLine: 1, Body: "x"})

	p := s.Progress([]string{"a.ts", "b.ts", "c.ts"})
	if p.Total != 3 {
		t.Errorf("Total = %d, want 3", p.Total)
	}
	if p.Reviewed != 2 {
		t.Errorf("Reviewed = %d, want 2 (viewed + commented)", p.Reviewed)
	}
	if p.Comments != 1 {
		t.Errorf("Comments = %d, want 1", p.Comments)
	}
	if p.Pending != 1 {
		t.Errorf("Pending = %d, want 1", p.Pending)
	}
}

func TestSession_ProgressIgnoresFilesNoLongerInTheDiff(t *testing.T) {
	s := NewSession()
	s.MarkViewed("gone.ts")

	p := s.Progress([]string{"a.ts"})
	if p.Reviewed != 0 {
		t.Errorf("Reviewed = %d, want 0 — gone.ts is not in the diff any more", p.Reviewed)
	}
	if p.Total != 1 {
		t.Errorf("Total = %d, want 1", p.Total)
	}
}
