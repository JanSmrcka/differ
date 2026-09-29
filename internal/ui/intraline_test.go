package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/theme"
)

// Split view pairs a removed line with the added line that replaced it, but
// said nothing about what actually differs between them — so a one-character
// change and a rewritten line looked identical, and the reader had to compare
// two 80-column strings by eye.

func TestIntraLine_FindsWhatChangedBetweenTwoLines(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                  string
		old, new              string
		start, endOld, endNew int
	}{
		{
			name: "one argument changed",
			old:  "doThing(ctx, 3)", new: "doThing(ctx, 9)",
			start: 13, endOld: 14, endNew: 14,
		},
		{
			name: "something inserted",
			old:  "call(a)", new: "call(a, b)",
			start: 6, endOld: 6, endNew: 9,
		},
		{
			name: "something removed",
			old:  "call(a, b)", new: "call(a)",
			start: 6, endOld: 9, endNew: 6,
		},
		{
			name: "identical lines have no range",
			old:  "same", new: "same",
			start: 4, endOld: 4, endNew: 4,
		},
		{
			name: "nothing in common",
			old:  "abc", new: "xyz",
			start: 0, endOld: 3, endNew: 3,
		},
		{
			name: "only the tail differs",
			old:  "prefix-old", new: "prefix-new",
			start: 7, endOld: 10, endNew: 10,
		},
		{
			name: "an empty side",
			old:  "", new: "added",
			start: 0, endOld: 0, endNew: 5,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			start, endOld, endNew := changedRange(c.old, c.new)
			if start != c.start || endOld != c.endOld || endNew != c.endNew {
				t.Errorf("changedRange(%q, %q) = (%d, %d, %d), want (%d, %d, %d)",
					c.old, c.new, start, endOld, endNew, c.start, c.endOld, c.endNew)
			}
		})
	}
}

// The ranges have to be usable as rune slices without going out of bounds or
// crossing over, whatever the pair.
func TestIntraLine_TheRangeIsAlwaysUsable(t *testing.T) {
	t.Parallel()
	lines := []string{"", "a", "abc", "abcdef", "日本語", "🇨🇿flag", "a🇨🇿c", strings.Repeat("x", 40)}
	for _, old := range lines {
		for _, next := range lines {
			start, endOld, endNew := changedRange(old, next)
			o, n := []rune(old), []rune(next)
			if start < 0 || start > len(o) || start > len(n) {
				t.Errorf("changedRange(%q, %q): start %d out of range", old, next, start)
			}
			if endOld < start || endOld > len(o) {
				t.Errorf("changedRange(%q, %q): endOld %d out of range", old, next, endOld)
			}
			if endNew < start || endNew > len(n) {
				t.Errorf("changedRange(%q, %q): endNew %d out of range", old, next, endNew)
			}
		}
	}
}

// Which pairs get emphasis is the decision worth pinning. How it is *painted*
// cannot be asserted here: the suite has no TTY, so lipgloss emits neither
// colour nor the underline, and the rendered bytes are identical either way.
// The test below covers what does survive — that the text itself is never
// altered — and this one covers the choice.
func TestIntraLine_OnlyAReplacementWithSomethingInCommonIsEmphasised(t *testing.T) {
	t.Parallel()
	removed := func(s string) *DiffLine { return &DiffLine{Type: LineRemoved, Content: s} }
	added := func(s string) *DiffLine { return &DiffLine{Type: LineAdded, Content: s} }
	context := func(s string) *DiffLine { return &DiffLine{Type: LineContext, Content: s} }

	cases := []struct {
		name        string
		left, right *DiffLine
		want        bool
	}{
		{
			name: "one argument changed",
			left: removed("call(ctx, 3)"), right: added("call(ctx, 9)"), want: true,
		},
		{
			name: "a rewritten line has nothing to point at",
			left: removed("aaaa"), right: added("bbbb"), want: false,
		},
		{
			name: "a plain deletion is not a replacement",
			left: removed("gone"), right: nil, want: false,
		},
		{
			name: "a plain insertion is not a replacement",
			left: nil, right: added("new"), want: false,
		},
		{
			name: "a context line is paired with itself",
			left: context("same"), right: context("same"), want: false,
		},
	}

	th := theme.Themes["dark"]
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := NewDiffRenderer(ParseDiff("@@ -1,1 +1,1 @@\n context\n"), "a.ts", NewStyles(th), th, 80)
			oldSpan, newSpan := r.changedSpans(c.left, c.right)
			if got := oldSpan.marks() || newSpan.marks(); got != c.want {
				t.Errorf("emphasised = %v, want %v (spans %+v %+v)", got, c.want, oldSpan, newSpan)
			}
		})
	}

	// And switching it off is honoured.
	r := NewDiffRenderer(ParseDiff("@@ -1,1 +1,1 @@\n context\n"), "a.ts", NewStyles(th), th, 80)
	r.SetIntraLine(false)
	if o, n := r.changedSpans(removed("call(ctx, 3)"), added("call(ctx, 9)")); o.marks() || n.marks() {
		t.Error("SetIntraLine(false) still produced a span")
	}
}

// Emphasis is paint, so it must never change a single character of the line,
// and the rows must stay exactly the panel width.
func TestIntraLine_TheLineItselfIsUntouched(t *testing.T) {
	t.Parallel()
	raw := "@@ -1,1 +1,1 @@\n-const x = doThing(ctx, 3);\n+const x = doThing(ctx, 9);\n"
	th := theme.Themes["dark"]
	r := NewDiffRenderer(ParseDiff(raw), "a.ts", NewStyles(th), th, 100)
	r.SetSplit(true)

	body := r.Content(-1)
	plain := stripANSI(body)
	for _, want := range []string{"const x = doThing(ctx, 3);", "const x = doThing(ctx, 9);"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the line was altered, %q is missing:\n%s", want, plain)
		}
	}
	for _, row := range strings.Split(body, "\n") {
		if got := lipgloss.Width(row); got != 100 {
			t.Errorf("row is %d columns, want 100: %q", got, stripANSI(row))
		}
	}
	if r.DisplayRows() != len(strings.Split(body, "\n")) {
		t.Errorf("DisplayRows() = %d but Content() has %d rows", r.DisplayRows(), len(strings.Split(body, "\n")))
	}
}

// split cuts a token at the span's boundaries. Whatever the cut, the pieces
// have to reassemble into exactly what went in — this is the one place a
// rendering bug could silently drop or duplicate characters.
func TestIntraLine_SplittingATokenLosesNothing(t *testing.T) {
	t.Parallel()
	texts := []string{"", "a", "abcdef", "日本語のテキスト", "a🇨🇿c"}
	for _, text := range texts {
		runes := len([]rune(text))
		for from := 0; from <= runes+1; from++ {
			for to := 0; to <= runes+1; to++ {
				for offset := 0; offset <= 2; offset++ {
					s := span{from: from, to: to}
					var rebuilt strings.Builder
					emphasised := 0
					for _, p := range s.split(text, offset) {
						rebuilt.WriteString(p.text)
						if p.emph {
							emphasised++
						}
					}
					if rebuilt.String() != text {
						t.Fatalf("span{%d,%d}.split(%q, %d) rebuilt as %q", from, to, text, offset, rebuilt.String())
					}
					if emphasised > 1 {
						t.Fatalf("span{%d,%d}.split(%q, %d) produced %d emphasised pieces", from, to, text, offset, emphasised)
					}
				}
			}
		}
	}
}

// Under --no-color the emphasis has no colour to use, so it must still not
// corrupt the line: the text comes through whole.
func TestIntraLine_WithoutColourTheTextSurvives(t *testing.T) {
	t.Parallel()
	raw := "@@ -1,1 +1,1 @@\n-const x = 3;\n+const x = 9;\n"
	th := theme.NoColorTheme()
	r := NewDiffRenderer(ParseDiff(raw), "a.ts", NewStyles(th), th, 80)
	r.SetSplit(true)

	plain := stripANSI(r.Content(-1))
	for _, want := range []string{"const x = 3;", "const x = 9;"} {
		if !strings.Contains(plain, want) {
			t.Errorf("without colour, %q is missing:\n%s", want, plain)
		}
	}
}
