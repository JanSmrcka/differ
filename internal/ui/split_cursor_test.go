package ui

import (
	"strings"
	"testing"

	"github.com/jansmrcka/differ/internal/testutil"
)

// The headline guarantee of #37: a cursor position means the same thing in
// both views, so a comment made in split resolves where it was made.
func TestCursorAddressing_IdenticalInUnifiedAndSplit(t *testing.T) {
	styles, th := testStyles()
	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)

	unified := NewDiffRenderer(parsed, "src.ts", styles, th, 120)
	split := NewDiffRenderer(parsed, "src.ts", styles, th, 120)
	split.SetSplit(true)

	if unified.LineCount() != split.LineCount() {
		t.Fatalf("LineCount differs: unified %d, split %d", unified.LineCount(), split.LineCount())
	}

	for i := 0; i < unified.LineCount(); i++ {
		ua, uok := unified.Parsed().AddressOf(i)
		sa, sok := split.Parsed().AddressOf(i)
		if uok != sok || ua != sa {
			t.Errorf("line %d addresses differently: unified %+v(%v), split %+v(%v)", i, ua, uok, sa, sok)
		}
	}
}

func TestSplitRenderer_CollapsesPairedLinesOntoOneRow(t *testing.T) {
	styles, th := testStyles()
	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)
	r := NewDiffRenderer(parsed, "src.ts", styles, th, 120)
	r.SetSplit(true)

	if r.DisplayRows() >= r.LineCount() {
		t.Errorf("split should pair removed/added lines: %d rows for %d lines", r.DisplayRows(), r.LineCount())
	}

	removed := lineIndexOf(t, parsed, LineRemoved, "  const user = getUser(id)")
	added := lineIndexOf(t, parsed, LineAdded, "  const user = await getUser(id)")
	rRow, ok1 := r.RowFor(removed)
	aRow, ok2 := r.RowFor(added)
	if !ok1 || !ok2 {
		t.Fatal("both sides of a replacement must map to a row")
	}
	if rRow != aRow {
		t.Errorf("replaced line pair should share a row, got %d and %d", rRow, aRow)
	}
}

func TestSplitRenderer_CursorMarksTheRowContainingTheLine(t *testing.T) {
	styles, th := testStyles()
	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)
	r := NewDiffRenderer(parsed, "src.ts", styles, th, 120)
	r.SetSplit(true)

	added := lineIndexOf(t, parsed, LineAdded, "  const user = await getUser(id)")
	row, _ := r.RowFor(added)

	rows := strings.Split(r.Content(added), "\n")
	if !strings.Contains(rows[row], cursorMarker) {
		t.Errorf("row %d should carry the cursor marker: %q", row, rows[row])
	}
	if strings.Count(r.Content(added), cursorMarker) != 1 {
		t.Error("exactly one row should be marked")
	}
}

func TestParseNewFile_IsAddressableLikeADiff(t *testing.T) {
	parsed := ParseNewFile("alpha\nbeta\ngamma\n")

	if len(parsed.Lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(parsed.Lines))
	}
	if len(parsed.Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(parsed.Hunks))
	}

	addr, ok := parsed.AddressOf(1)
	if !ok {
		t.Fatal("!ok")
	}
	if addr.NewLine != 2 || addr.OldLine != -1 {
		t.Errorf("address = old %d new %d, want old -1 new 2", addr.OldLine, addr.NewLine)
	}
	if addr.HunkIndex != 0 {
		t.Errorf("HunkIndex = %d, want 0", addr.HunkIndex)
	}
	if parsed.FirstCommentableLine() != 0 {
		t.Errorf("FirstCommentableLine = %d, want 0", parsed.FirstCommentableLine())
	}
}

func TestParseNewFile_Empty(t *testing.T) {
	parsed := ParseNewFile("")
	if len(parsed.Lines) != 0 {
		t.Errorf("empty file should produce no lines, got %d", len(parsed.Lines))
	}
	if _, ok := parsed.AddressOf(0); ok {
		t.Error("empty diff should not address line 0")
	}
}

func TestParseNewFile_NoTrailingNewline(t *testing.T) {
	parsed := ParseNewFile("only")
	if len(parsed.Lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(parsed.Lines))
	}
	if parsed.Lines[0].Content != "only" {
		t.Errorf("content = %q", parsed.Lines[0].Content)
	}
}
