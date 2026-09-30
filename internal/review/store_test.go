package review

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixedKeys answers content questions from a table, which is what the UI's
// file fingerprints look like from here.
func fixedKeys(table map[string]string) Keys {
	return func(files []string) map[string]string { return table }
}

func TestStore_PendingCommentSurvivesWhenTheFileHasNotMoved(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())
	s := NewSession()
	s.Add(Comment{File: "a.ts", StartLine: 12, EndLine: 12, Anchor: "x := 1", Body: "why 1?"})

	if err := st.Save(s, fixedKeys(map[string]string{"a.ts": "k1"})); err != nil {
		t.Fatalf("Save: %v", err)
	}

	back := st.Load(fixedKeys(map[string]string{"a.ts": "k1"}))
	if back == nil {
		t.Fatal("nothing was restored")
	}
	got := back.CommentsFor("a.ts")
	if len(got) != 1 {
		t.Fatalf("restored %d comments, want 1", len(got))
	}
	if got[0].Body != "why 1?" || got[0].StartLine != 12 || got[0].Anchor != "x := 1" {
		t.Errorf("restored comment = %+v", got[0])
	}
	if got[0].State != StatePending {
		t.Errorf("restored State = %v, want pending", got[0].State)
	}
}

func TestStore_CommentsAreDroppedWhenTheirFileWasRewritten(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())
	s := NewSession()
	s.Add(Comment{File: "moved.ts", StartLine: 3, EndLine: 3, Body: "about the old code"})
	s.Add(Comment{File: "still.ts", StartLine: 7, EndLine: 7, Body: "about code that is still there"})
	if err := st.Save(s, fixedKeys(map[string]string{"moved.ts": "k1", "still.ts": "k2"})); err != nil {
		t.Fatalf("Save: %v", err)
	}

	back := st.Load(fixedKeys(map[string]string{"moved.ts": "rewritten", "still.ts": "k2"}))
	if back == nil {
		t.Fatal("nothing was restored")
	}
	if n := back.CountFor("moved.ts"); n != 0 {
		t.Errorf("moved.ts came back with %d comments, want none", n)
	}
	// And with them goes everything that was concluded about the file: it has
	// to be read again, so it is unreviewed rather than merely uncommented.
	if got := back.FileStateOf("moved.ts"); got != FileUnreviewed {
		t.Errorf("moved.ts state = %v, want unreviewed", got)
	}
	if n := back.CountFor("still.ts"); n != 1 {
		t.Errorf("still.ts came back with %d comments, want 1", n)
	}
}

// A file nothing can be said about — gone from the changeset, or unreadable —
// is treated as moved. The gate is only worth having if it fails closed.
func TestStore_CommentsAreDroppedWhenTheFilesKeyIsUnknown(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())
	s := NewSession()
	s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "x"})
	// A delivery, so that the session itself still has a reason to exist and
	// this test is about the comment rather than about the empty case.
	s.RecordDelivery(Delivery{At: time.Now(), Target: "tmux", Comments: []string{"c0"}})
	if err := st.Save(s, fixedKeys(map[string]string{"a.ts": "k1"})); err != nil {
		t.Fatalf("Save: %v", err)
	}

	back := st.Load(fixedKeys(nil))
	if back == nil {
		t.Fatal("nothing was restored")
	}
	if n := back.CountFor("a.ts"); n != 0 {
		t.Errorf("a.ts came back with %d comments, want none", n)
	}
}

func TestStore_SentCommentsComeBackAsHistoryAndNotAsWork(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())
	s := NewSession()
	c := s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "already told them"})
	s.MarkSent([]string{c.ID})
	s.RecordDelivery(Delivery{At: time.Now(), Target: "tmux", Comments: []string{c.ID}, Files: []string{"a.ts"}})
	keys := fixedKeys(map[string]string{"a.ts": "k1"})
	if err := st.Save(s, keys); err != nil {
		t.Fatalf("Save: %v", err)
	}

	back := st.Load(keys)
	if back == nil {
		t.Fatal("nothing was restored")
	}
	if n := back.CountFor("a.ts"); n != 0 {
		t.Errorf("a delivered comment came back as %d live comments, want 0", n)
	}
	h := back.History()
	if len(h) != 1 {
		t.Fatalf("restored %d deliveries, want 1", len(h))
	}
	if h[0].Target != "tmux" || len(h[0].Comments) != 1 || h[0].Files[0] != "a.ts" {
		t.Errorf("restored delivery = %+v", h[0])
	}
}

// The history is a record of what the agent has already been told. Losing it
// is what makes someone send the same review twice, so unlike the comments it
// does not depend on any file still being what it was.
func TestStore_HistorySurvivesEvenWhenEveryFileWasRewritten(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())
	s := NewSession()
	c := s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "x"})
	s.RecordDelivery(Delivery{At: time.Now(), Target: "clipboard", Comments: []string{c.ID}, Files: []string{"a.ts"}, Err: "clipboard is empty"})
	if err := st.Save(s, fixedKeys(map[string]string{"a.ts": "k1"})); err != nil {
		t.Fatalf("Save: %v", err)
	}

	back := st.Load(fixedKeys(map[string]string{"a.ts": "rewritten"}))
	if back == nil {
		t.Fatal("nothing was restored")
	}
	h := back.History()
	if len(h) != 1 {
		t.Fatalf("restored %d deliveries, want 1", len(h))
	}
	// Failures included: a send that went nowhere is the one worth looking up.
	if h[0].OK() || h[0].Err != "clipboard is empty" {
		t.Errorf("restored delivery lost its failure: %+v", h[0])
	}
}

// Timestamps are what the history is read by, so they have to survive the
// round trip rather than come back as the moment differ was restarted.
func TestStore_DeliveryKeepsItsTime(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())
	s := NewSession()
	when := time.Date(2025, 3, 4, 14, 22, 6, 0, time.UTC)
	s.RecordDelivery(Delivery{At: when, Target: "tmux", Comments: []string{"c1"}})
	if err := st.Save(s, fixedKeys(nil)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	back := st.Load(fixedKeys(nil))
	if back == nil {
		t.Fatal("nothing was restored")
	}
	if got := back.History()[0].At; !got.Equal(when) {
		t.Errorf("delivery At = %v, want %v", got, when)
	}
}

func TestStore_NothingSavedRestoresNothing(t *testing.T) {
	t.Parallel()
	if got := NewStore(t.TempDir()).Load(fixedKeys(nil)); got != nil {
		t.Errorf("Load of an absent file = %v, want nil", got)
	}
}

// A file cut in half by a kill, or written by a differ that speaks a different
// format, costs the comments and nothing else. The session has to start.
func TestStore_AnUnreadableFileIsIgnoredRatherThanFatal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, content string }{
		{"truncated", `{"version":1,"comments":[{"id":"c1","fi`},
		{"empty", ""},
		{"not json at all", "\x00\x01garbage"},
		{"a future format", `{"version":99,"comments":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := NewStore(t.TempDir())
			if err := os.MkdirAll(filepath.Dir(st.Path()), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(st.Path(), []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}

			if got := st.Load(fixedKeys(nil)); got != nil {
				t.Errorf("Load of %s = %v, want nil", tc.name, got)
			}
		})
	}
}

// The history names comments by id. Handing a new comment the id of one that
// was dropped on restore would make an entry in the history describe something
// nobody ever sent.
func TestStore_NewCommentsDoNotReuseAnIdTheHistoryRefersTo(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())
	s := NewSession()
	sent := s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "sent"})
	s.Add(Comment{File: "a.ts", StartLine: 2, EndLine: 2, Body: "pending"})
	s.MarkSent([]string{sent.ID})
	s.RecordDelivery(Delivery{At: time.Now(), Target: "tmux", Comments: []string{sent.ID}})
	keys := fixedKeys(map[string]string{"a.ts": "k1"})
	if err := st.Save(s, keys); err != nil {
		t.Fatalf("Save: %v", err)
	}

	back := st.Load(keys)
	if back == nil {
		t.Fatal("nothing was restored")
	}
	fresh := back.Add(Comment{File: "a.ts", StartLine: 3, EndLine: 3, Body: "new"})
	if fresh.ID == sent.ID {
		t.Errorf("a new comment took %q, the id the history refers to", fresh.ID)
	}
	if _, taken := back.Get("c2"); taken && fresh.ID == "c2" {
		t.Errorf("a new comment took %q, which a restored comment already has", fresh.ID)
	}
}

// Temp file plus rename, with the temp file in the same directory so the
// rename is a rename and not a copy. Nothing may be left behind: the directory
// is inside .git, and a drift of half-written files there is litter nobody
// will ever look at.
func TestStore_SaveCreatesItsDirectoryAndLeavesNoTempFiles(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())
	s := NewSession()
	s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "x"})

	for range 3 {
		if err := st.Save(s, fixedKeys(map[string]string{"a.ts": "k1"})); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	entries, err := os.ReadDir(filepath.Dir(st.Path()))
	if err != nil {
		t.Fatalf("the store did not create its directory: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "review.json" {
		t.Errorf("directory holds %v, want just review.json", names)
	}
}

// A review that has been emptied — every comment deleted, nothing ever sent —
// leaves a file behind, and reading it back must not produce a session that
// exists only to hold nothing. Whether the last run wrote a file is not
// something the next one should be able to tell.
func TestStore_AnEmptiedReviewRestoresNothing(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())
	s := NewSession()
	c := s.Add(Comment{File: "a.ts", StartLine: 1, EndLine: 1, Body: "x"})
	s.Remove(c.ID)

	if err := st.Save(s, fixedKeys(nil)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := st.Load(fixedKeys(nil)); got != nil {
		t.Errorf("Load = %v, want nil", got)
	}
}
