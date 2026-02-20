package ui

import (
	"strings"
	"testing"

	"github.com/jansmrcka/differ/internal/theme"
)

func TestPairLines_ContextOnly(t *testing.T) {
	lines := []DiffLine{
		{Type: LineContext, Content: "a", OldNum: 1, NewNum: 1},
		{Type: LineContext, Content: "b", OldNum: 2, NewNum: 2},
	}
	pairs := PairLines(lines)
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(pairs))
	}
	for i, p := range pairs {
		if p.Left == nil || p.Right == nil {
			t.Fatalf("pair %d: expected both sides non-nil", i)
		}
		if p.Left.Content != p.Right.Content {
			t.Errorf("pair %d: left %q != right %q", i, p.Left.Content, p.Right.Content)
		}
	}
}

func TestPairLines_RemovedThenAdded(t *testing.T) {
	lines := []DiffLine{
		{Type: LineRemoved, Content: "old1", OldNum: 1, NewNum: -1},
		{Type: LineRemoved, Content: "old2", OldNum: 2, NewNum: -1},
		{Type: LineAdded, Content: "new1", OldNum: -1, NewNum: 1},
	}
	pairs := PairLines(lines)
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(pairs))
	}
	// First pair: removed left, added right
	if pairs[0].Left.Content != "old1" || pairs[0].Right.Content != "new1" {
		t.Errorf("pair 0: got left=%q right=%q", pairs[0].Left.Content, pairs[0].Right.Content)
	}
	// Second pair: removed left, nil right
	if pairs[1].Left.Content != "old2" || pairs[1].Right != nil {
		t.Errorf("pair 1: got left=%q right=%v", pairs[1].Left.Content, pairs[1].Right)
	}
}

func TestPairLines_OrphanAdded(t *testing.T) {
	lines := []DiffLine{
		{Type: LineContext, Content: "ctx", OldNum: 1, NewNum: 1},
		{Type: LineAdded, Content: "new", OldNum: -1, NewNum: 2},
	}
	pairs := PairLines(lines)
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(pairs))
	}
	if pairs[1].Left != nil {
		t.Error("orphan added: expected nil left")
	}
	if pairs[1].Right == nil || pairs[1].Right.Content != "new" {
		t.Error("orphan added: expected right with content 'new'")
	}
}

func TestPairLines_HunkHeader(t *testing.T) {
	lines := []DiffLine{
		{Type: LineHunkHeader, Content: "func main()", OldNum: -1, NewNum: -1},
		{Type: LineContext, Content: "a", OldNum: 10, NewNum: 10},
	}
	pairs := PairLines(lines)
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(pairs))
	}
	if pairs[0].Left == nil || pairs[0].Left.Type != LineHunkHeader {
		t.Error("expected hunk header on left")
	}
	if pairs[0].Right != nil {
		t.Error("expected nil right for hunk header")
	}
}

func TestPairLines_EqualRemovedAdded(t *testing.T) {
	lines := []DiffLine{
		{Type: LineRemoved, Content: "old", OldNum: 1, NewNum: -1},
		{Type: LineAdded, Content: "new", OldNum: -1, NewNum: 1},
	}
	pairs := PairLines(lines)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].Left.Content != "old" || pairs[0].Right.Content != "new" {
		t.Errorf("got left=%q right=%q", pairs[0].Left.Content, pairs[0].Right.Content)
	}
}

func TestPairLines_MoreAddedThanRemoved(t *testing.T) {
	lines := []DiffLine{
		{Type: LineRemoved, Content: "old", OldNum: 1, NewNum: -1},
		{Type: LineAdded, Content: "new1", OldNum: -1, NewNum: 1},
		{Type: LineAdded, Content: "new2", OldNum: -1, NewNum: 2},
	}
	pairs := PairLines(lines)
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(pairs))
	}
	if pairs[0].Left.Content != "old" || pairs[0].Right.Content != "new1" {
		t.Errorf("pair 0: left=%q right=%q", pairs[0].Left.Content, pairs[0].Right.Content)
	}
	if pairs[1].Left != nil || pairs[1].Right.Content != "new2" {
		t.Errorf("pair 1: left=%v right=%q", pairs[1].Left, pairs[1].Right.Content)
	}
}

func TestPairLines_Empty(t *testing.T) {
	pairs := PairLines(nil)
	if len(pairs) != 0 {
		t.Fatalf("expected 0 pairs, got %d", len(pairs))
	}
}

func testStyles() (Styles, theme.Theme) {
	th := theme.Themes["dark"]
	return NewStyles(th), th
}

func TestRenderSplitDiff_ContainsSeparator(t *testing.T) {
	parsed := ParsedDiff{Lines: []DiffLine{
		{Type: LineContext, Content: "hello", OldNum: 1, NewNum: 1},
	}}
	styles, th := testStyles()
	result := RenderSplitDiff(parsed, "test.go", styles, th, 100)
	if !strings.Contains(result, "│") {
		t.Error("split diff should contain │ separator")
	}
}

func TestRenderSplitDiff_Binary(t *testing.T) {
	parsed := ParsedDiff{Binary: true}
	styles, th := testStyles()
	result := RenderSplitDiff(parsed, "test.bin", styles, th, 100)
	if !strings.Contains(result, "Binary") {
		t.Error("binary file should show binary message")
	}
}

func TestRenderNewFileSplit_ContainsSeparator(t *testing.T) {
	styles, th := testStyles()
	result := RenderNewFileSplit("line1\nline2", "test.go", styles, th, 100)
	if !strings.Contains(result, "│") {
		t.Error("split new file should contain │ separator")
	}
}

func TestRenderSplitSide_Nil(t *testing.T) {
	styles, th := testStyles()
	result := renderSplitSide(nil, "test.go", styles, th, 40, true)
	if len(result) == 0 {
		t.Error("nil side should produce padding, not empty")
	}
	// Should be all spaces
	if strings.TrimSpace(result) != "" {
		t.Errorf("nil side should be blank, got %q", result)
	}
}

func TestRenderSplitSide_Added(t *testing.T) {
	styles, th := testStyles()
	dl := &DiffLine{Type: LineAdded, Content: "new line", OldNum: -1, NewNum: 5}
	result := renderSplitSide(dl, "test.go", styles, th, 50, false)
	if len(result) == 0 {
		t.Error("added line should produce output")
	}
}

func TestRenderSplitSide_Removed(t *testing.T) {
	styles, th := testStyles()
	dl := &DiffLine{Type: LineRemoved, Content: "old line", OldNum: 3, NewNum: -1}
	result := renderSplitSide(dl, "test.go", styles, th, 50, true)
	if len(result) == 0 {
		t.Error("removed line should produce output")
	}
}

func TestRenderSplitSide_ZeroWidth(t *testing.T) {
	styles, th := testStyles()
	dl := &DiffLine{Type: LineContext, Content: "x", OldNum: 1, NewNum: 1}
	// Should not panic with tiny panelW
	result := renderSplitSide(dl, "test.go", styles, th, 5, true)
	if len(result) == 0 {
		t.Error("should produce some output even with tiny width")
	}
}

// --- extractHunkContext ---

func TestExtractHunkContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"with_func", "@@ -13,6 +13,7 @@ func main() {", "func main() {"},
		{"no_context", "@@ -13,6 +13,7 @@", "-13,6 +13,7"},
		{"empty_context", "@@ -1,3 +1,4 @@  ", "-1,3 +1,4"},
		{"complex", "@@ -100,20 +105,25 @@ type Foo struct {", "type Foo struct {"},
		{"bare", "@@@@", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractHunkContext(tt.input)
			if got != tt.want {
				t.Errorf("extractHunkContext(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- parseHunkHeader ---

func TestParseHunkHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantOld int
		wantNew int
	}{
		{"basic", "@@ -10,5 +20,8 @@", 10, 20},
		{"no_count", "@@ -1 +1 @@", 1, 1},
		{"large", "@@ -100,50 +200,60 @@ func foo()", 100, 200},
		{"missing", "no hunk", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			old, new := 0, 0
			parseHunkHeader(tt.input, &old, &new)
			if old != tt.wantOld {
				t.Errorf("oldNum=%d, want %d", old, tt.wantOld)
			}
			if new != tt.wantNew {
				t.Errorf("newNum=%d, want %d", new, tt.wantNew)
			}
		})
	}
}

// --- fmtLineNum ---

func TestFmtLineNum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		n    int
		want string
	}{
		{"negative", -1, "    "},
		{"zero", 0, "   0"},
		{"one", 1, "   1"},
		{"four_digit", 9999, "9999"},
		{"five_digit", 10000, "10000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fmtLineNum(tt.n)
			if got != tt.want {
				t.Errorf("fmtLineNum(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

// --- ParseDiff ---

func TestParseDiff_SimpleHunk(t *testing.T) {
	t.Parallel()
	raw := `diff --git a/f.go b/f.go
index abc..def 100644
--- a/f.go
+++ b/f.go
@@ -1,3 +1,4 @@
 context
-removed
+added1
+added2`
	parsed := ParseDiff(raw)
	if parsed.Binary {
		t.Error("should not be binary")
	}
	if len(parsed.Lines) == 0 {
		t.Fatal("expected parsed lines")
	}
	// Should have: hunk header, context, removed, added1, added2
	types := make(map[DiffLineType]int)
	for _, l := range parsed.Lines {
		types[l.Type]++
	}
	if types[LineHunkHeader] != 1 {
		t.Errorf("hunk headers=%d, want 1", types[LineHunkHeader])
	}
	if types[LineRemoved] != 1 {
		t.Errorf("removed=%d, want 1", types[LineRemoved])
	}
	if types[LineAdded] != 2 {
		t.Errorf("added=%d, want 2", types[LineAdded])
	}
}

func TestParseDiff_BinaryFile(t *testing.T) {
	t.Parallel()
	raw := "Binary files a/img.png and b/img.png differ"
	parsed := ParseDiff(raw)
	if !parsed.Binary {
		t.Error("should detect binary file")
	}
}

func TestParseDiff_MultipleHunks(t *testing.T) {
	t.Parallel()
	raw := `@@ -1,3 +1,3 @@
 ctx1
-old1
+new1
@@ -10,3 +10,3 @@
 ctx2
-old2
+new2`
	parsed := ParseDiff(raw)
	hunks := 0
	for _, l := range parsed.Lines {
		if l.Type == LineHunkHeader {
			hunks++
		}
	}
	if hunks != 2 {
		t.Errorf("expected 2 hunks, got %d", hunks)
	}
}

func TestParseDiff_SkipsHeaders(t *testing.T) {
	t.Parallel()
	raw := `diff --git a/f.go b/f.go
index abc..def 100644
new file mode 100644
--- /dev/null
+++ b/f.go
@@ -0,0 +1,1 @@
+new line`
	parsed := ParseDiff(raw)
	for _, l := range parsed.Lines {
		if l.Type == LineFileHeader {
			t.Error("file headers should be skipped")
		}
	}
}

func TestParseDiff_Empty(t *testing.T) {
	t.Parallel()
	parsed := ParseDiff("")
	if parsed.Binary {
		t.Error("empty should not be binary")
	}
	if len(parsed.Lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(parsed.Lines))
	}
}

func TestParseDiff_Truncation(t *testing.T) {
	t.Parallel()
	// Build a diff that exceeds maxDiffLines
	var b strings.Builder
	b.WriteString("@@ -1,20000 +1,20000 @@\n")
	for range maxDiffLines + 10 {
		b.WriteString("+line\n")
	}
	parsed := ParseDiff(b.String())
	// Should be capped at maxDiffLines+1 (the truncation marker)
	if len(parsed.Lines) > maxDiffLines+2 {
		t.Errorf("expected truncation, got %d lines", len(parsed.Lines))
	}
	// Last line should be the truncation marker
	last := parsed.Lines[len(parsed.Lines)-1]
	if last.Type != LineHunkHeader || !strings.Contains(last.Content, "truncated") {
		t.Errorf("expected truncation marker, got %+v", last)
	}
}

// --- Render functions ---

func TestRenderBinaryFile(t *testing.T) {
	t.Parallel()
	styles, _ := testStyles()
	result := RenderBinaryFile(styles, 80)
	if !strings.Contains(result, "Binary") {
		t.Error("should contain 'Binary'")
	}
}

func TestRenderDiff_Basic(t *testing.T) {
	t.Parallel()
	parsed := ParsedDiff{Lines: []DiffLine{
		{Type: LineHunkHeader, Content: "func main()", OldNum: -1, NewNum: -1},
		{Type: LineContext, Content: "ctx", OldNum: 1, NewNum: 1},
		{Type: LineAdded, Content: "new", OldNum: -1, NewNum: 2},
	}}
	styles, th := testStyles()
	result := RenderDiff(parsed, "test.go", styles, th, 100)
	if result == "" {
		t.Error("expected non-empty render")
	}
}

func TestRenderDiff_Binary(t *testing.T) {
	t.Parallel()
	parsed := ParsedDiff{Binary: true}
	styles, th := testStyles()
	result := RenderDiff(parsed, "test.bin", styles, th, 100)
	if !strings.Contains(result, "Binary") {
		t.Error("binary diff should show binary message")
	}
}

func TestRenderNewFile_Basic(t *testing.T) {
	t.Parallel()
	styles, th := testStyles()
	result := RenderNewFile("line1\nline2\nline3", "test.go", styles, th, 100)
	if result == "" {
		t.Error("expected non-empty render")
	}
	// Should have 3 lines of output
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}
