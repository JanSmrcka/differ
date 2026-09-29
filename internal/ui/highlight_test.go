package ui

import (
	"testing"

	"github.com/alecthomas/chroma/v2"
)

func TestTokenForeground_Set(t *testing.T) {
	t.Parallel()
	entry := chroma.StyleEntry{Colour: chroma.MustParseColour("#ff0000")}
	got := tokenForeground(entry)
	if got == "" {
		t.Error("expected non-empty foreground for set colour")
	}
}

func TestTokenForeground_Unset(t *testing.T) {
	t.Parallel()
	entry := chroma.StyleEntry{}
	got := tokenForeground(entry)
	if got != "" {
		t.Errorf("expected empty foreground for unset colour, got %q", got)
	}
}

func TestHighlightLine_Empty(t *testing.T) {
	t.Parallel()
	// Empty content should return empty whatever the palette
	got := highlightLine(chromaStyleFor("monokai"), "", "test.go", "")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// The palette is a parameter now, not a package variable. It used to be
// global, which meant this test and the theme picker's wrote to the same
// place — and with both marked t.Parallel, three runs in four disagreed about
// which theme was in force.
func TestHighlightLine_GoCode(t *testing.T) {
	t.Parallel()
	_, th := testStyles()

	got := highlightLine(chromaStyleFor(th.ChromaStyle), "func main() {}", "main.go", "")
	if got == "" {
		t.Error("expected non-empty highlighted output")
	}
}

// No palette means the line comes back exactly as it went in, which is what
// --no-color asks for.
func TestHighlightLine_NoPaletteLeavesTheLineAlone(t *testing.T) {
	t.Parallel()
	const line = "func main() {}"
	if got := highlightLine(nil, line, "main.go", "#000000"); got != line {
		t.Errorf("with no palette the line came back as %q", got)
	}
}
