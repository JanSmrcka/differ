package ui

import (
	"strings"
	"testing"

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
	th := theme.Themes["dark"]
	r := NewDiffRenderer(parsed, "src.ts", NewStyles(th), th, width)
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
		for _, row := range rows {
			plain := stripANSI(row)
			if i := strings.Index(plain, needle); i >= 0 {
				return i
			}
		}
		t.Fatalf("no row contains %q:\n%s", needle, strings.Join(rows, "\n"))
		return -1
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
