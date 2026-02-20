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
	// Empty content should return empty regardless of chroma state
	got := highlightLine("", "test.go", "")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestHighlightLine_GoCode(t *testing.T) {
	t.Parallel()
	_, th := testStyles()
	initChromaStyle(th.ChromaStyle)

	got := highlightLine("func main() {}", "main.go", "")
	if got == "" {
		t.Error("expected non-empty highlighted output")
	}
}
