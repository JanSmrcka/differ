package ui

import (
	"strings"
	"testing"

	"github.com/jansmrcka/differ/internal/testutil"
)

func newTestRenderer(t *testing.T, fixture string) (*DiffRenderer, ParsedDiff) {
	t.Helper()
	styles, th := testStyles()
	parsed := ParseDiff(testutil.Fixture(t, fixture).Diff)
	return NewDiffRenderer(parsed, "src.ts", styles, th, 80), parsed
}

func TestDiffRenderer_MarksOnlyTheCursorLine(t *testing.T) {
	r, parsed := newTestRenderer(t, "multi_hunk")
	cursor := parsed.FirstCommentableLine()

	lines := strings.Split(strings.TrimRight(r.Content(cursor), "\n"), "\n")
	if len(lines) != len(parsed.Lines) {
		t.Fatalf("rendered %d lines, want %d", len(lines), len(parsed.Lines))
	}

	for i, l := range lines {
		marked := strings.Contains(l, cursorMarker)
		if i == cursor && !marked {
			t.Errorf("cursor line %d is not marked: %q", i, l)
		}
		if i != cursor && marked {
			t.Errorf("non-cursor line %d is marked: %q", i, l)
		}
	}
}

func TestDiffRenderer_CursorMoves(t *testing.T) {
	r, parsed := newTestRenderer(t, "multi_hunk")
	a := parsed.FirstCommentableLine()
	b := parsed.Hunks[1].StartLine + 1

	first := strings.Split(r.Content(a), "\n")
	second := strings.Split(r.Content(b), "\n")

	if !strings.Contains(second[b], cursorMarker) {
		t.Errorf("after moving, line %d should be marked", b)
	}
	if strings.Contains(second[a], cursorMarker) {
		t.Errorf("after moving, line %d should no longer be marked", a)
	}
	// Untouched lines must render identically — the cache must not drift.
	for i := range first {
		if i == a || i == b {
			continue
		}
		if first[i] != second[i] {
			t.Fatalf("line %d changed when only the cursor moved:\n before: %q\n after:  %q", i, first[i], second[i])
		}
	}
}

func TestDiffRenderer_NoCursor(t *testing.T) {
	r, _ := newTestRenderer(t, "multi_hunk")
	if strings.Contains(r.Content(-1), cursorMarker) {
		t.Error("cursor -1 should render no marker")
	}
}

func TestDiffRenderer_OutOfRangeCursorRendersNoMarker(t *testing.T) {
	r, parsed := newTestRenderer(t, "multi_hunk")
	if strings.Contains(r.Content(len(parsed.Lines)+10), cursorMarker) {
		t.Error("out-of-range cursor should not mark any line")
	}
}

func TestDiffRenderer_CursorChangesOnlyTheGutter(t *testing.T) {
	r, parsed := newTestRenderer(t, "multi_hunk")
	added := lineIndexOf(t, parsed, LineAdded, "  const user = await getUser(id)")

	plain := strings.Split(r.Content(-1), "\n")[added]
	marked := strings.Split(r.Content(added), "\n")[added]

	if !strings.Contains(marked, "await getUser(id)") {
		t.Errorf("cursor line lost its content: %q", marked)
	}
	// Everything past the gutter must be byte-identical, so highlighting and
	// alignment cannot shift when the cursor lands on a line.
	if plain[len(blankGutter()):] != marked[len(cursorMarker)+1:] {
		t.Errorf("cursor changed more than the gutter:\n plain:  %q\n marked: %q", plain, marked)
	}
}

func TestDiffRenderer_Binary(t *testing.T) {
	r, _ := newTestRenderer(t, "binary_file")
	if !strings.Contains(r.Content(0), "Binary file") {
		t.Error("binary diff should render its placeholder")
	}
}
