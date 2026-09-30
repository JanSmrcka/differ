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

// The agent's editor integration understands a reference, so the payload
// carries one.
//
// sidekick.nvim — the working reference on this machine — sends
// "@src/cache.ts:L12" and lets Claude Code read the file. differ sends the
// hunk as well, which is the more useful thing for a review, but without the
// reference the agent has to parse "File:" and "Line:" out of prose to know
// where to go.
func TestFormatComment_CarriesAReferenceTheAgentUnderstands(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		c    Comment
		want string
	}{
		{
			name: "one line on the new side",
			c:    Comment{File: "src/cache.ts", Side: SideNew, StartLine: 12, EndLine: 12, Body: "x"},
			want: "@src/cache.ts :L12",
		},
		{
			name: "a range",
			c:    Comment{File: "src/cache.ts", Side: SideNew, StartLine: 12, EndLine: 20, Body: "x"},
			want: "@src/cache.ts :L12-L20",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FormatComment(tc.c)
			if !strings.Contains(got, tc.want) {
				t.Errorf("payload carries no %q:\n%s", tc.want, got)
			}
		})
	}
}

// The reference is a line of its own, so a parser can find it without reading
// the prose around it.
func TestFormatComment_TheReferenceIsItsOwnLine(t *testing.T) {
	t.Parallel()
	got := FormatComment(Comment{
		File: "src/cache.ts", Side: SideNew, StartLine: 12, EndLine: 12,
		Body: "this drops the error", Excerpt: "-  old\n+  new",
	})

	var found bool
	for _, line := range strings.Split(got, "\n") {
		if strings.TrimSpace(line) == "@src/cache.ts :L12" {
			found = true
		}
	}
	if !found {
		t.Errorf("the reference is not on a line of its own:\n%s", got)
	}
	// And everything that was there before still is.
	for _, want := range []string{"File: src/cache.ts", "Line: 12", "Changed code:", "-  old", "this drops the error"} {
		if !strings.Contains(got, want) {
			t.Errorf("the payload lost %q:\n%s", want, got)
		}
	}
}

// The space before the colon is the whole point of the format.
//
// sidekick.nvim's commit d570e1f ("different format that should work for most
// cli tools") added the space and the L prefix together: "@path:12" made the
// agent's @-mention resolver read the whole token as a filename, fail to stat
// it, and attach nothing. An earlier version of this code took the L and left
// the space out, which is the broken shape with extra characters.
func TestReference_KeepsTheSpaceCliToolsNeed(t *testing.T) {
	t.Parallel()
	got := Reference(Comment{File: "src/cache.ts", Side: SideNew, StartLine: 12, EndLine: 12})
	const want = "@src/cache.ts :L12"
	if got != want {
		t.Errorf("Reference = %q, want %q", got, want)
	}
	if strings.Contains(got, "ts:L") {
		t.Errorf("the path is glued to the location: %q", got)
	}
}

// A comment on deleted code carries no reference.
//
// The reference resolves against the file as it is now, so an old-side line
// number lands on whatever occupies that line today — unrelated code, or
// nothing at all in a deleted file. Two comments in one payload, one on a
// removed line and one on a context line, produced byte-identical references
// to different things.
func TestReference_SaysNothingAboutDeletedCode(t *testing.T) {
	t.Parallel()
	old := Comment{File: "src/gone.ts", Side: SideOld, StartLine: 7, EndLine: 7, Body: "why?"}
	if got := Reference(old); got != "" {
		t.Errorf("Reference for deleted code = %q, want none", got)
	}

	// The payload still says where it was, and which side.
	body := FormatComment(old)
	for _, want := range []string{"File: src/gone.ts", "Line: 7 (old)", "why?"} {
		if !strings.Contains(body, want) {
			t.Errorf("the payload lost %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "@src/gone.ts") {
		t.Errorf("a reference was emitted for deleted code:\n%s", body)
	}
}

// StartLine is what a single-line reference reports. Using EndLine passes
// today only because Session.Add normalises them to be equal.
func TestReference_ASingleLineReportsItsStartLine(t *testing.T) {
	t.Parallel()
	got := Reference(Comment{File: "a.ts", Side: SideNew, StartLine: 5, EndLine: 9})
	if got != "@a.ts :L5-L9" {
		t.Errorf("Reference = %q", got)
	}
	// And with them equal, the single-line form.
	if got := Reference(Comment{File: "a.ts", Side: SideNew, StartLine: 5, EndLine: 5}); got != "@a.ts :L5" {
		t.Errorf("Reference = %q, want @a.ts :L5", got)
	}
}
