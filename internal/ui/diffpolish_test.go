package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/theme"
)

// Diff rendering under pressure: a minified bundle, a five-digit line number,
// a hunk boundary, trailing whitespace. Each one used to distort the layout or
// disappear.

// renderedRows renders a parsed diff at a width and returns its rows with
// colour stripped, which is how a row's real column count is measured.
func renderedRows(t *testing.T, parsed ParsedDiff, width int) []string {
	t.Helper()
	return renderedRowsIn(t, parsed, width, false)
}

// renderedRowsIn does the same in either view. Split was the half with no
// tests at all, which is how the two views drifted apart before.
func renderedRowsIn(t *testing.T, parsed ParsedDiff, width int, split bool) []string {
	t.Helper()
	th := theme.Themes["dark"]
	r := NewDiffRenderer(parsed, "src.ts", NewStyles(th), th, width)
	r.SetSplit(split)
	return strings.Split(r.Content(-1), "\n")
}

// A long line used to be padded but never cut, so the terminal wrapped it and
// every row below shifted — the line-number column included.
func TestDiffRender_ALongLineIsCutToTheWidth(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 500)
	parsed := ParseDiff("@@ -1,1 +1,1 @@\n+" + long + "\n")

	for _, width := range []int{40, 80, 120} {
		for _, row := range renderedRows(t, parsed, width) {
			if got := lipgloss.Width(row); got > width {
				t.Errorf("width %d: a row is %d columns wide", width, got)
			}
		}
	}
}

// Cutting a line silently would be worse than wrapping it: the reader has to
// know there is more.
func TestDiffRender_ACutLineSaysSo(t *testing.T) {
	t.Parallel()
	parsed := ParseDiff("@@ -1,1 +1,1 @@\n+" + strings.Repeat("x", 500) + "\n")
	rows := renderedRows(t, parsed, 80)

	body := stripANSI(strings.Join(rows, "\n"))
	if !strings.Contains(body, truncationMarker) {
		t.Errorf("a cut line carries no marker:\n%s", body)
	}
}

// Line numbers were fixed at four columns, so a file over 9999 lines pushed
// its own code one column right and the gutter stopped lining up.
func TestDiffRender_TheCodeColumnSurvivesFiveDigitLineNumbers(t *testing.T) {
	t.Parallel()
	raw := "@@ -998,2 +998,2 @@\n context AAA\n" +
		"@@ -10000,2 +10000,2 @@\n context BBB\n"
	rows := renderedRows(t, ParseDiff(raw), 100)

	at := func(needle string) int {
		col := columnOf(rows, needle)
		if col < 0 {
			t.Fatalf("no row contains %q:\n%s", needle, strings.Join(rows, "\n"))
		}
		return col
	}

	if a, b := at("AAA"), at("BBB"); a != b {
		t.Errorf("code starts at column %d on a 3-digit line and %d on a 5-digit one", a, b)
	}
	// And the number itself must still be there in full.
	if !strings.Contains(stripANSI(strings.Join(rows, "\n")), "10000") {
		t.Error("the five-digit line number was clipped")
	}
}

// Hunks used to run into each other: a "···" prefix and nothing else, so two
// hunks a line apart looked like one continuous stretch of code.
func TestDiffRender_HunksAreSeparatedByARule(t *testing.T) {
	t.Parallel()
	raw := "@@ -1,2 +1,2 @@ func one()\n context a\n" +
		"@@ -40,2 +40,2 @@ func two()\n context b\n"
	rows := renderedRows(t, ParseDiff(raw), 100)

	var headers []string
	for _, row := range rows {
		plain := stripANSI(row)
		if strings.Contains(plain, "func one()") || strings.Contains(plain, "func two()") {
			headers = append(headers, plain)
		}
	}
	if len(headers) != 2 {
		t.Fatalf("found %d hunk headers, want 2:\n%s", len(headers), strings.Join(rows, "\n"))
	}
	for _, h := range headers {
		if !strings.Contains(h, strings.Repeat(horizontalRule, 4)) {
			t.Errorf("a hunk header carries no rule:\n%q", h)
		}
		if got := lipgloss.Width(h); got != 100 {
			t.Errorf("a hunk header is %d columns, want the full 100 so the break reads as a break:\n%q", got, h)
		}
	}
}

// A hunk header's context can be a long function signature. It has to be cut
// like any other line, and still leave the break visible.
func TestDiffRender_ALongHunkContextIsCut(t *testing.T) {
	t.Parallel()
	raw := "@@ -1,2 +1,2 @@ func " + strings.Repeat("Long", 80) + "()\n context a\n"
	for _, row := range renderedRows(t, ParseDiff(raw), 80) {
		if got := lipgloss.Width(row); got > 80 {
			t.Errorf("row is %d columns wide: %q", got, stripANSI(row))
		}
	}
}

// Trailing whitespace is invisible in a diff and is exactly the sort of thing
// a reviewer is expected to catch.
func TestDiffRender_TrailingWhitespaceIsVisible(t *testing.T) {
	t.Parallel()
	parsed := ParseDiff("@@ -1,2 +1,2 @@\n+const a = 1;   \n+const b = 2;\n")
	body := stripANSI(strings.Join(renderedRows(t, parsed, 80), "\n"))

	if !strings.Contains(body, "const a = 1;"+strings.Repeat(whitespaceMarker, 3)) {
		t.Errorf("trailing whitespace is not marked:\n%s", body)
	}
	if strings.Contains(body, "const b = 2;"+whitespaceMarker) {
		t.Errorf("a line with no trailing whitespace was marked:\n%s", body)
	}
}

// Context lines carry whatever the file already had; marking those would flag
// the whole file rather than the change.
func TestDiffRender_OnlyChangedLinesAreMarkedForWhitespace(t *testing.T) {
	t.Parallel()
	parsed := ParseDiff("@@ -1,2 +1,2 @@\n untouched   \n+changed   \n")
	body := stripANSI(strings.Join(renderedRows(t, parsed, 80), "\n"))

	if strings.Contains(body, "untouched"+whitespaceMarker) {
		t.Errorf("a context line was marked:\n%s", body)
	}
	if !strings.Contains(body, "changed"+strings.Repeat(whitespaceMarker, 3)) {
		t.Errorf("the changed line was not marked:\n%s", body)
	}
}

// columnOf is where needle starts, measured in display columns. strings.Index
// gives a byte offset, and the gutter contains multi-byte glyphs — "···" is
// two bytes a rune — so a byte offset reads three columns too far.
func columnOf(rows []string, needle string) int {
	for _, row := range rows {
		plain := stripANSI(row)
		if i := strings.Index(plain, needle); i >= 0 {
			return lipgloss.Width(plain[:i])
		}
	}
	return -1
}

// A width sweep, not three sample widths. Every renderer had a floor it
// silently exceeded — the line-number block alone is wider than a 10-column
// panel — and the three values the first test checked all sat above it.
func TestDiffRender_NoRowExceedsThePanelAtAnyWidth(t *testing.T) {
	t.Parallel()
	raw := "@@ -9998,3 +9999,3 @@ func handler() {\n" +
		" context AAA\n" +
		"-old   \n" +
		"+" + strings.Repeat("wide", 200) + "\n" +
		" 日本語のテキストはここにあります\n"
	parsed := ParseDiff(raw)

	for _, split := range []bool{false, true} {
		for width := 1; width <= 120; width++ {
			for _, row := range renderedRowsIn(t, parsed, width, split) {
				if got := lipgloss.Width(row); got > width {
					t.Fatalf("split=%v width=%d: row is %d columns: %q",
						split, width, got, stripANSI(row))
				}
			}
		}
	}
}

// The renderer addresses rows by index: DisplayRows, rowOf and RowFor all
// assume one entry per row of output. A row that secretly contains a newline
// breaks that for every row below it, and the cursor then highlights the wrong
// line.
//
// Chroma is what produced one: it appends a newline to a trailing-whitespace
// token, and a context line ending in spaces was handed to it whole.
func TestDiffRender_EveryRowIsOneRow(t *testing.T) {
	t.Parallel()
	raw := "@@ -1,4 +1,4 @@\n untouched   \n+changed   \n-gone   \n context2\n"

	for _, split := range []bool{false, true} {
		th := theme.Themes["dark"]
		r := NewDiffRenderer(ParseDiff(raw), "src.ts", NewStyles(th), th, 80)
		r.SetSplit(split)

		rows := strings.Split(r.Content(-1), "\n")
		if r.DisplayRows() != len(rows) {
			t.Errorf("split=%v: DisplayRows() = %d but Content() has %d rows",
				split, r.DisplayRows(), len(rows))
		}
		for i, row := range rows {
			if strings.Contains(row, "\n") {
				t.Errorf("split=%v: row %d contains a newline: %q", split, i, stripANSI(row))
			}
			if got := lipgloss.Width(row); got != 80 {
				t.Errorf("split=%v: row %d is %d columns, want 80: %q", split, i, got, stripANSI(row))
			}
		}
	}
}

// Split view has to cut long lines and mark trailing whitespace exactly as
// unified does — that shared behaviour is the whole point of stylesFor and
// renderCode, and it had no test.
func TestDiffRender_SplitViewCutsAndMarksLikeUnified(t *testing.T) {
	t.Parallel()
	raw := "@@ -1,2 +1,2 @@\n-short   \n+" + strings.Repeat("y", 300) + "\n"
	body := stripANSI(strings.Join(renderedRowsIn(t, ParseDiff(raw), 100, true), "\n"))

	if !strings.Contains(body, truncationMarker) {
		t.Errorf("split view did not mark the cut line:\n%s", body)
	}
	if !strings.Contains(body, "short"+strings.Repeat(whitespaceMarker, 3)) {
		t.Errorf("split view did not mark trailing whitespace:\n%s", body)
	}
}

// A hunk header spans the row in both views, so its text has to start where
// that view's code starts — five columns off in split, which is what made it
// look like a header for the wrong line.
func TestDiffRender_TheHunkHeaderAlignsWithTheCodeInBothViews(t *testing.T) {
	t.Parallel()
	// The marker is the whole content of the context line, so its column is
	// where the code column starts — locating text *inside* the line would
	// measure the wrong thing.
	raw := "@@ -1,2 +1,2 @@ func handler() {\n MARKER\n"

	for _, split := range []bool{false, true} {
		rows := renderedRowsIn(t, ParseDiff(raw), 100, split)
		header, code := columnOf(rows, "func handler()"), columnOf(rows, "MARKER")
		if header < 0 || code < 0 {
			t.Fatalf("split=%v: header at %d, code at %d:\n%s", split, header, code, strings.Join(rows, "\n"))
		}
		if header != code {
			t.Errorf("split=%v: hunk text starts at column %d, code at %d", split, header, code)
		}
	}
}

// The first version cut a line by dropping one rune at a time and re-measuring
// the whole prefix: 8.8 seconds for an 80,000-column line. Content() and
// SetSplit both re-render from inside Update, so a single j onto a minified
// line blocked the event loop for seconds.
func TestDiffRender_ALongLineRendersQuickly(t *testing.T) {
	t.Parallel()
	parsed := ParseDiff("@@ -1,1 +1,1 @@\n+" + strings.Repeat("x", 150_000) + "\n")

	done := make(chan struct{})
	go func() {
		defer close(done)
		th := theme.Themes["dark"]
		r := NewDiffRenderer(parsed, "bundle.js", NewStyles(th), th, 120)
		_ = r.Content(1)
		r.SetSplit(true)
		_ = r.Content(1)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("rendering one 150,000-column line took over 2s")
	}
}
