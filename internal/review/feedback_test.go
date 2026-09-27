package review

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jansmrcka/differ/internal/testutil"
)

func loginComment() Comment {
	return Comment{
		ID:        "c1",
		File:      "src/auth/login.ts",
		Side:      SideNew,
		StartLine: 43,
		EndLine:   43,
		HunkIndex: 0,
		Anchor:    "  const user = getUser(id)",
		Excerpt: " export function load(id: string) {\n" +
			"-  const user = getUser(id)\n" +
			"+  const user = await getUser(id)\n" +
			"   return user\n",
		Body:  "This should remain awaited. We need the resolved user here.",
		State: StatePending,
	}
}

func TestFormatFeedback_SingleLineComment(t *testing.T) {
	testutil.Golden(t, "feedback_single_line", FormatFeedback([]Comment{loginComment()}))
}

func TestFormatFeedback_HunkComment(t *testing.T) {
	c := loginComment()
	c.StartLine, c.EndLine = 42, 46
	c.Body = "Keep this awaited. The caller depends on the resolved value."
	testutil.Golden(t, "feedback_hunk", FormatFeedback([]Comment{c}))
}

func TestFormatFeedback_MultipleFiles(t *testing.T) {
	a := loginComment()
	b := Comment{
		ID: "c2", File: "src/api/client.ts", Side: SideOld, StartLine: 10, EndLine: 10,
		Excerpt: "-  return fetch(url)\n+  return await fetch(url)\n",
		Body:    "Same problem here.", State: StatePending,
	}
	testutil.Golden(t, "feedback_multi_file", FormatFeedback([]Comment{b, a}))
}

func TestFormatFeedback_MultilineBody(t *testing.T) {
	c := loginComment()
	c.Body = "Two problems:\n\n1. missing await\n2. no error handling"
	testutil.Golden(t, "feedback_multiline_body", FormatFeedback([]Comment{c}))
}

func TestFormatFeedback_DeletedFileComment(t *testing.T) {
	c := Comment{
		ID: "c1", File: "src/legacy/old.ts", Side: SideOld, StartLine: 1, EndLine: 1,
		Excerpt: "-export const deprecated = true\n",
		Body:    "Why was this removed?", State: StatePending,
	}
	testutil.Golden(t, "feedback_deleted_file", FormatFeedback([]Comment{c}))
}

func TestFormatFeedback_IsDeterministic(t *testing.T) {
	cs := []Comment{loginComment()}
	first := FormatFeedback(cs)
	for i := 0; i < 20; i++ {
		if got := FormatFeedback(cs); got != first {
			t.Fatal("FormatFeedback is not deterministic")
		}
	}
}

func TestFormatFeedback_OrderIndependent(t *testing.T) {
	a := loginComment()
	b := a
	b.ID, b.File, b.StartLine = "c2", "src/api/client.ts", 10

	forward := FormatFeedback([]Comment{a, b})
	backward := FormatFeedback([]Comment{b, a})
	if forward != backward {
		t.Errorf("input order changed the output:\n%s\n---\n%s", forward, backward)
	}
}

func TestFormatFeedback_ContainsNoANSI(t *testing.T) {
	out := FormatFeedback([]Comment{loginComment()})
	if strings.Contains(out, "\x1b") {
		t.Errorf("output contains ANSI escapes:\n%q", out)
	}
}

func TestFormatFeedback_Empty(t *testing.T) {
	if got := FormatFeedback(nil); got != "" {
		t.Errorf("no comments should produce no output, got %q", got)
	}
}

func TestFormatFeedback_DistinguishesOldAndNewSide(t *testing.T) {
	newSide := FormatFeedback([]Comment{loginComment()})
	if !strings.Contains(newSide, "(new)") {
		t.Errorf("new-side comment should say so:\n%s", newSide)
	}

	c := loginComment()
	c.Side = SideOld
	if !strings.Contains(FormatFeedback([]Comment{c}), "(old)") {
		t.Error("old-side comment should say so")
	}
}

func TestFormatFeedback_OmitsAnEmptyExcerpt(t *testing.T) {
	c := loginComment()
	c.Excerpt = ""
	out := FormatFeedback([]Comment{c})
	if strings.Contains(out, "Changed code:") {
		t.Errorf("no excerpt means no 'Changed code' section:\n%s", out)
	}
	if !strings.Contains(out, c.Body) {
		t.Error("the comment body must still be present")
	}
}

func TestFormatFeedback_ScalesToManyComments(t *testing.T) {
	var cs []Comment
	for i := 0; i < 50; i++ {
		c := loginComment()
		c.StartLine, c.EndLine = i+1, i+1
		cs = append(cs, c)
	}
	out := FormatFeedback(cs)
	if n := strings.Count(out, "Comment:"); n != 50 {
		t.Errorf("got %d comment sections, want 50", n)
	}
}

// #8: IDs are "c1".."c10", so a lexicographic tiebreak puts c10 before c9.
// Comments on the same file and line must come out in the order written.
func TestFormatFeedback_TiebreakUsesCreationOrderNotStringOrder(t *testing.T) {
	s := NewSession()
	var want []string
	for i := 1; i <= 12; i++ {
		body := fmt.Sprintf("note %d", i)
		// Every comment sits on the same file and line, so only the tiebreak
		// decides the order.
		s.Add(Comment{File: "a.ts", Side: SideNew, StartLine: 1, EndLine: 1, Body: body})
		want = append(want, body)
	}

	out := FormatFeedback(s.Comments())
	var positions []int
	for _, body := range want {
		idx := strings.Index(out, body)
		if idx < 0 {
			t.Fatalf("comment %q missing from output", body)
		}
		positions = append(positions, idx)
	}
	for i := 1; i < len(positions); i++ {
		if positions[i] < positions[i-1] {
			t.Errorf("comment %q appears before %q — tiebreak is not creation order", want[i], want[i-1])
		}
	}
}
