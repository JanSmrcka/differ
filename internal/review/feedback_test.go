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
		Locate:    LocateLine,
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
		ID: "c2", File: "src/api/client.ts", Side: SideOld, Locate: LocateFile,
		StartLine: 10, EndLine: 10,
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
		ID: "c1", File: "src/legacy/old.ts", Side: SideOld, Locate: LocateNone,
		StartLine: 1, EndLine: 1,
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

// Which side a comment is on only needs saying when the reference cannot
// take the agent there. A new-side comment whose line resolves is addressed
// by `@path :Ln`, and "(new)" would be restating it; an old-side one is a
// line in the file *before* the change, which nothing can open, so it has to
// be said.
func TestFormatFeedback_SaysWhichSideOnlyWhenItHasTo(t *testing.T) {
	t.Parallel()
	resolvable := loginComment()
	resolvable.Locate = LocateLine
	if out := FormatFeedback([]Comment{resolvable}); strings.Contains(out, "(new)") {
		t.Errorf("a resolvable line restates its side:\n%s", out)
	}

	old := loginComment()
	old.Side, old.Locate = SideOld, LocateFile
	out := FormatFeedback([]Comment{old})
	if !strings.Contains(out, "(old)") {
		t.Errorf("an old-side comment does not say so:\n%s", out)
	}
	if !strings.Contains(out, "Changed code:") {
		t.Errorf("an old-side comment lost the excerpt, which is the only "+
			"thing that says which code is meant:\n%s", out)
	}
}

func TestFormatFeedback_OmitsAnEmptyExcerpt(t *testing.T) {
	c := loginComment()
	c.Locate = LocateFile // the form that carries an excerpt at all
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
	// Counted by the reference, not by a "Comment:" label: the resolvable
	// form has no label, because the whole payload is the reference and the
	// body.
	if n := strings.Count(out, "@src/auth/login.ts :L"); n != 50 {
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
// "@src/cache.ts :L12" and lets Claude Code read the file. differ sends the
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
			c:    Comment{File: "src/cache.ts", Locate: LocateLine, StartLine: 12, EndLine: 12, Body: "x"},
			want: "@src/cache.ts :L12",
		},
		{
			name: "a range",
			c:    Comment{File: "src/cache.ts", Locate: LocateLine, StartLine: 12, EndLine: 20, Body: "x"},
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
		File: "src/cache.ts", Side: SideNew, Locate: LocateLine, StartLine: 12, EndLine: 12,
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
	// And nothing restates it. This used to assert that `File:`, `Line:` and
	// the excerpt were all still there; they were the duplication #90
	// removed.
	for _, gone := range []string{"File:", "Line:", "Changed code:"} {
		if strings.Contains(got, gone) {
			t.Errorf("the payload still carries %q, which the reference "+
				"already says:\n%s", gone, got)
		}
	}
	if !strings.Contains(got, "this drops the error") {
		t.Errorf("the payload lost the body:\n%s", got)
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
	got := Reference(Comment{File: "src/cache.ts", Locate: LocateLine, StartLine: 12, EndLine: 12})
	const want = "@src/cache.ts :L12"
	if got != want {
		t.Errorf("Reference = %q, want %q", got, want)
	}
	if strings.Contains(got, "ts:L") {
		t.Errorf("the path is glued to the location: %q", got)
	}
}

// How much of a reference a comment gets depends on what still resolves.
//
// The first version keyed this on Side, which was wrong twice over: it emitted
// a line reference under -s, where the diff's new side is the index and the
// working tree has usually moved on, and it emitted nothing for an old-side
// comment on a file that is still there — losing the file as well as the line,
// when sidekick.nvim has a bare "@path" form for exactly that.
func TestReference_SaysAsMuchAsStillResolves(t *testing.T) {
	t.Parallel()
	base := Comment{File: "src/cache.ts", StartLine: 12, EndLine: 12, Body: "x"}

	for _, tc := range []struct {
		name   string
		locate Locate
		want   string
	}{
		{"the line resolves", LocateLine, "@src/cache.ts :L12"},
		{"only the file resolves", LocateFile, "@src/cache.ts"},
		{"nothing resolves", LocateNone, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := base
			c.Locate = tc.locate
			if got := Reference(c); got != tc.want {
				t.Errorf("Reference = %q, want %q", got, tc.want)
			}
		})
	}
}

// A comment with nothing to point at still says where it was, in prose.
func TestFormatComment_KeepsTheProseWhenThereIsNoReference(t *testing.T) {
	t.Parallel()
	body := FormatComment(Comment{
		File: "src/gone.ts", Side: SideOld, Locate: LocateNone,
		StartLine: 7, EndLine: 7, Body: "why?",
	})
	for _, want := range []string{"File: src/gone.ts", "Line: 7 (old)", "why?"} {
		if !strings.Contains(body, want) {
			t.Errorf("the payload lost %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "@src/gone.ts") {
		t.Errorf("a reference was emitted for a deleted file:\n%s", body)
	}
}

// StartLine is what a single-line reference reports. Using EndLine passes
// today only because Session.Add normalises them to be equal.
func TestReference_ASingleLineReportsItsStartLine(t *testing.T) {
	t.Parallel()
	if got := Reference(Comment{File: "a.ts", Locate: LocateLine, StartLine: 5, EndLine: 9}); got != "@a.ts :L5-L9" {
		t.Errorf("a range = %q", got)
	}
	if got := Reference(Comment{File: "a.ts", Locate: LocateLine, StartLine: 5, EndLine: 5}); got != "@a.ts :L5" {
		t.Errorf("one line = %q, want @a.ts :L5", got)
	}
	// The case that actually pins it. Both of the above take the branch they
	// take whichever field it reads, so an earlier version of this test could
	// not tell StartLine from EndLine — Session.Add normalises them, so the
	// only way to exercise the difference is to build the comment by hand
	// with EndLine behind StartLine.
	if got := Reference(Comment{File: "a.ts", Locate: LocateLine, StartLine: 9, EndLine: 5}); got != "@a.ts :L9" {
		t.Errorf("one line = %q, want @a.ts :L9 — the reference reports StartLine", got)
	}
}

// A path that contains a space cannot be written as an @-reference at all.
//
// The space before the colon exists because the resolver tokenises on
// whitespace; a space inside the path hands it the token "@my" and a file
// that does not exist. Saying nothing is better than saying something wrong
// — the File: line below still carries the path in full.
func TestReference_APathWithASpaceGetsNoReference(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"my notes.ts", "src/two words/a.ts", "a\tb.ts"} {
		c := Comment{File: path, Locate: LocateLine, StartLine: 1, EndLine: 1}
		if got := Reference(c); got != "" {
			t.Errorf("Reference for %q = %q, which the resolver cannot read", path, got)
		}
	}
	// And a path with no space still gets one, including a non-ASCII name:
	// core.quotepath=false means those arrive intact and resolve fine.
	c := Comment{File: "žluťoučký.ts", Locate: LocateLine, StartLine: 1, EndLine: 1}
	if got := Reference(c); got != "@žluťoučký.ts :L1" {
		t.Errorf("Reference for a non-ASCII path = %q", got)
	}
}

// LocateUnknown is the zero value and must degrade to a file reference. Two
// halves, both untested: that the zero value is not LocateLine, and that
// LocateUnknown is treated as file-only. Either one alone lets a Comment
// built without the field claim a line nobody checked — which is exactly
// what a comment decoded from storage is.
func TestReference_TheZeroValueClaimsNoLine(t *testing.T) {
	t.Parallel()
	if LocateUnknown != 0 {
		t.Errorf("LocateUnknown = %d, want 0 — the zero value must be the safe one", LocateUnknown)
	}
	var zero Locate
	if zero == LocateLine {
		t.Error("the zero Locate is LocateLine, so a Comment built without it claims a line")
	}
	got := Reference(Comment{File: "a.ts", StartLine: 12, EndLine: 12})
	if got != "@a.ts" {
		t.Errorf("a Comment with no Locate references %q, want the file alone", got)
	}
}

// A comment with no file has nothing to point at, whatever it claims.
func TestReference_NoFileMeansNoReference(t *testing.T) {
	t.Parallel()
	if got := Reference(Comment{Locate: LocateLine, StartLine: 3, EndLine: 3}); got != "" {
		t.Errorf("Reference with no file = %q", got)
	}
}

// A comment whose line resolves is the reference and the body, and nothing
// else.
//
// The payload used to say the same thing three times: the reference carries
// the file and the line, `File:` repeated the file, `Line:` repeated the
// line, and the excerpt showed code the agent could read for itself from the
// reference it had just been given. The comment — the only part only the
// reviewer could write — came last and smallest.
func TestFormatComment_AResolvableLineNeedsNothingButTheReference(t *testing.T) {
	t.Parallel()
	got := FormatComment(Comment{
		File: "src/api/client.ts", Side: SideNew, Locate: LocateLine,
		StartLine: 14, EndLine: 14,
		Excerpt: " ctx\n-old\n+new\n ctx\n",
		Body:    "co je tohle?",
	})

	want := "@src/api/client.ts :L14\n\nco je tohle?\n"
	if got != want {
		t.Errorf("payload =\n%q\nwant\n%q", got, want)
	}
}

// The degraded form keeps everything the reference cannot carry, and still
// does not repeat what it can.
func TestFormatComment_AnUnresolvableLineKeepsItsContext(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		locate  Locate
		side    Side
		wantRef string
	}{
		{"old side", LocateFile, SideOld, "@src/a.ts"},
		{"index line numbers", LocateFile, SideNew, "@src/a.ts"},
		{"nothing on disk", LocateNone, SideNew, ""},
	} {
		c := Comment{
			File: "src/a.ts", Side: tc.side, Locate: tc.locate,
			StartLine: 10, EndLine: 10,
			Excerpt: "-  return fetch(url)\n+  return await fetch(url)\n",
			Body:    "this needs awaiting too",
		}
		got := FormatComment(c)

		if !strings.Contains(got, "Changed code:") {
			t.Errorf("%s: lost the excerpt, which is all that says which code "+
				"is meant:\n%s", tc.name, got)
		}
		if !strings.Contains(got, c.Body) {
			t.Errorf("%s: lost the body:\n%s", tc.name, got)
		}
		if !strings.Contains(got, "Line: 10") {
			t.Errorf("%s: does not say where it was:\n%s", tc.name, got)
		}

		// The file is named once: by the reference when there is one, in
		// prose when there is not. Never both.
		named := strings.Count(got, "src/a.ts")
		if named != 1 {
			t.Errorf("%s: names the file %d times:\n%s", tc.name, named, got)
		}
		if tc.wantRef != "" && !strings.HasPrefix(got, tc.wantRef+"\n") {
			t.Errorf("%s: does not lead with %q:\n%s", tc.name, tc.wantRef, got)
		}
		if tc.wantRef == "" && !strings.HasPrefix(got, "File: src/a.ts\n") {
			t.Errorf("%s: with no reference the file must be named in prose:\n%s",
				tc.name, got)
		}
	}
}
