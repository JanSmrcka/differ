package ui

import (
	"strings"
	"testing"

	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
)

func reviewModel(t *testing.T, fixture string) Model {
	t.Helper()
	m := diffModel(t, fixture, 20)
	m.mode = modeFileList
	updated, _ := m.Update(key("r"))
	return updated.(Model)
}

func cursorOn(t *testing.T, m Model, typ DiffLineType, content string) Model {
	t.Helper()
	return m.setCursor(lineIndexOf(t, m.renderer.Parsed(), typ, content))
}

func TestBuildLineComment_OnAddedLine(t *testing.T) {
	m := reviewModel(t, "multi_hunk")
	m = cursorOn(t, m, LineAdded, "  const user = await getUser(id)")

	c, ok := m.buildLineComment()
	if !ok {
		t.Fatal("buildLineComment reported failure on an added line")
	}
	if c.File != "src.ts" {
		t.Errorf("File = %q, want src.ts", c.File)
	}
	if c.Side != review.SideNew {
		t.Errorf("Side = %v, want new", c.Side)
	}
	if c.StartLine != 2 || c.EndLine != 2 {
		t.Errorf("lines = %d-%d, want 2-2", c.StartLine, c.EndLine)
	}
	if c.HunkIndex != 0 {
		t.Errorf("HunkIndex = %d, want 0", c.HunkIndex)
	}
	if c.Anchor != "  const user = await getUser(id)" {
		t.Errorf("Anchor = %q", c.Anchor)
	}
}

func TestBuildLineComment_OnRemovedLineUsesOldSide(t *testing.T) {
	m := reviewModel(t, "multi_hunk")
	m = cursorOn(t, m, LineRemoved, "  await persist(data)")

	c, ok := m.buildLineComment()
	if !ok {
		t.Fatal("!ok")
	}
	if c.Side != review.SideOld {
		t.Errorf("Side = %v, want old", c.Side)
	}
	if c.StartLine != 11 {
		t.Errorf("StartLine = %d, want 11", c.StartLine)
	}
}

func TestBuildLineComment_ExcerptShowsTheChangeWithMarkers(t *testing.T) {
	m := reviewModel(t, "multi_hunk")
	m = cursorOn(t, m, LineAdded, "  const user = await getUser(id)")

	c, _ := m.buildLineComment()

	if !strings.Contains(c.Excerpt, "-  const user = getUser(id)") {
		t.Errorf("excerpt missing the removed line:\n%s", c.Excerpt)
	}
	if !strings.Contains(c.Excerpt, "+  const user = await getUser(id)") {
		t.Errorf("excerpt missing the added line:\n%s", c.Excerpt)
	}
	if strings.Contains(c.Excerpt, "\x1b[") {
		t.Errorf("excerpt must be plain text, got ANSI:\n%q", c.Excerpt)
	}
	// The excerpt is the hunk, not the whole file.
	if strings.Contains(c.Excerpt, "persist(data)") {
		t.Errorf("excerpt leaked another hunk:\n%s", c.Excerpt)
	}
}

func TestBuildHunkComment_CoversTheWholeHunk(t *testing.T) {
	m := reviewModel(t, "multi_hunk")
	m = cursorOn(t, m, LineAdded, "  const user = await getUser(id)")

	c, ok := m.buildHunkComment()
	if !ok {
		t.Fatal("!ok")
	}
	if c.StartLine != 1 || c.EndLine != 5 {
		t.Errorf("hunk comment lines = %d-%d, want 1-5", c.StartLine, c.EndLine)
	}
	if c.HunkIndex != 0 {
		t.Errorf("HunkIndex = %d, want 0", c.HunkIndex)
	}
}

func TestBuildHunkComment_SecondHunk(t *testing.T) {
	m := reviewModel(t, "multi_hunk")
	m = cursorOn(t, m, LineAdded, "  persist(data)")

	c, _ := m.buildHunkComment()
	if c.StartLine != 8 || c.EndLine != 13 {
		t.Errorf("hunk comment lines = %d-%d, want 8-13", c.StartLine, c.EndLine)
	}
}

func TestBuildLineComment_OnUntrackedFile(t *testing.T) {
	m := diffModel(t, "multi_hunk", 20)
	parsed := ParseNewFile("alpha\nbeta\n")
	m.renderer = NewDiffRenderer(parsed, "new.ts", m.styles, m.theme, 80)
	m.files[0].change.Path = "new.ts"
	m = m.setCursor(1)

	c, ok := m.buildLineComment()
	if !ok {
		t.Fatal("!ok")
	}
	if c.StartLine != 2 || c.Side != review.SideNew {
		t.Errorf("address = line %d side %v, want line 2 side new", c.StartLine, c.Side)
	}
	if !strings.Contains(c.Excerpt, "+beta") {
		t.Errorf("excerpt = %q", c.Excerpt)
	}
}

func TestBuildComment_FailsWithoutARenderer(t *testing.T) {
	m := reviewModel(t, "multi_hunk")
	m.renderer = nil
	if _, ok := m.buildLineComment(); ok {
		t.Error("buildLineComment should fail with no diff loaded")
	}
	if _, ok := m.buildHunkComment(); ok {
		t.Error("buildHunkComment should fail with no diff loaded")
	}
}

func TestExcerpt_IsTruncatedForHugeHunks(t *testing.T) {
	var b strings.Builder
	b.WriteString("@@ -1,400 +1,400 @@\n")
	for i := 0; i < 400; i++ {
		b.WriteString("+line\n")
	}
	parsed := ParseDiff(b.String())
	got := excerptFor(parsed, parsed.Hunks[0])

	if len(got) > maxExcerptChars+200 {
		t.Errorf("excerpt is %d chars, should be capped near %d", len(got), maxExcerptChars)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("a truncated excerpt should say so:\n%s", got[len(got)-200:])
	}
}

func TestDiffFixtureCoverage_CommentOnEveryFixture(t *testing.T) {
	for _, f := range testutil.Fixtures() {
		if f.Binary {
			continue
		}
		t.Run(f.Name, func(t *testing.T) {
			m := diffModel(t, f.Name, 20)
			if m.renderer.LineCount() == 0 {
				t.Skip("no addressable lines")
			}
			m.mode = modeReview
			m.session = review.NewSession()
			m = m.setCursor(m.renderer.Parsed().FirstCommentableLine())
			if _, ok := m.buildLineComment(); !ok {
				t.Error("could not build a comment on this fixture")
			}
		})
	}
}
