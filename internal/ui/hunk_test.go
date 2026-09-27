package ui

import (
	"testing"

	"github.com/jansmrcka/differ/internal/testutil"
)

func TestParseDiff_ExposesHunks(t *testing.T) {
	f := testutil.Fixture(t, "multi_hunk")

	parsed := ParseDiff(f.Diff)

	if len(parsed.Hunks) != 2 {
		t.Fatalf("got %d hunks, want 2", len(parsed.Hunks))
	}

	first := parsed.Hunks[0]
	if first.Index != 0 {
		t.Errorf("first hunk Index = %d, want 0", first.Index)
	}
	if first.OldStart != 1 || first.OldCount != 5 {
		t.Errorf("first hunk old range = %d,%d want 1,5", first.OldStart, first.OldCount)
	}
	if first.NewStart != 1 || first.NewCount != 5 {
		t.Errorf("first hunk new range = %d,%d want 1,5", first.NewStart, first.NewCount)
	}

	second := parsed.Hunks[1]
	if second.Index != 1 {
		t.Errorf("second hunk Index = %d, want 1", second.Index)
	}
	if second.OldStart != 8 || second.OldCount != 6 {
		t.Errorf("second hunk old range = %d,%d want 8,6", second.OldStart, second.OldCount)
	}
	if second.Context != "function helper(a: number) {" {
		t.Errorf("second hunk Context = %q", second.Context)
	}
}

func TestParseDiff_HunkLineRangesCoverTheirLines(t *testing.T) {
	f := testutil.Fixture(t, "multi_hunk")
	parsed := ParseDiff(f.Diff)

	for _, h := range parsed.Hunks {
		if parsed.Lines[h.StartLine].Type != LineHunkHeader {
			t.Errorf("hunk %d StartLine %d is not a header", h.Index, h.StartLine)
		}
		if h.LastLine < h.StartLine {
			t.Errorf("hunk %d LastLine %d before StartLine %d", h.Index, h.LastLine, h.StartLine)
		}
		for i := h.StartLine + 1; i <= h.LastLine; i++ {
			if parsed.Lines[i].Type == LineHunkHeader {
				t.Errorf("hunk %d range spills into the next hunk at line %d", h.Index, i)
			}
		}
	}

	// The two ranges must be contiguous and cover every line.
	if parsed.Hunks[0].LastLine+1 != parsed.Hunks[1].StartLine {
		t.Errorf("hunk ranges are not contiguous: %d then %d", parsed.Hunks[0].LastLine, parsed.Hunks[1].StartLine)
	}
	if last := parsed.Hunks[1].LastLine; last != len(parsed.Lines)-1 {
		t.Errorf("last hunk ends at %d, want %d", last, len(parsed.Lines)-1)
	}
}

func TestParseDiff_SingleHunkFixtures(t *testing.T) {
	for _, name := range []string{"new_file", "deleted_file", "long_lines", "tabs_indent"} {
		t.Run(name, func(t *testing.T) {
			f := testutil.Fixture(t, name)
			parsed := ParseDiff(f.Diff)
			if len(parsed.Hunks) != 1 {
				t.Fatalf("got %d hunks, want 1", len(parsed.Hunks))
			}
		})
	}
}

func TestParseDiff_BinaryHasNoHunks(t *testing.T) {
	f := testutil.Fixture(t, "binary_file")
	parsed := ParseDiff(f.Diff)
	if !parsed.Binary {
		t.Fatal("expected Binary to be true")
	}
	if len(parsed.Hunks) != 0 {
		t.Errorf("binary diff has %d hunks, want 0", len(parsed.Hunks))
	}
}

// lineIndexOf finds the rendered-line index whose content matches, so tests
// describe positions by the code they point at rather than magic numbers.
func lineIndexOf(t *testing.T, p ParsedDiff, typ DiffLineType, content string) int {
	t.Helper()
	for i, l := range p.Lines {
		if l.Type == typ && l.Content == content {
			return i
		}
	}
	t.Fatalf("no %v line with content %q", typ, content)
	return -1
}

func TestAddressOf_AddedAndRemovedLines(t *testing.T) {
	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)

	removed := lineIndexOf(t, parsed, LineRemoved, "  const user = getUser(id)")
	addr, ok := parsed.AddressOf(removed)
	if !ok {
		t.Fatal("AddressOf returned !ok for a removed line")
	}
	if addr.OldLine != 2 || addr.NewLine != -1 {
		t.Errorf("removed line address = old %d new %d, want old 2 new -1", addr.OldLine, addr.NewLine)
	}
	if addr.HunkIndex != 0 {
		t.Errorf("removed line HunkIndex = %d, want 0", addr.HunkIndex)
	}

	added := lineIndexOf(t, parsed, LineAdded, "  const user = await getUser(id)")
	addr, ok = parsed.AddressOf(added)
	if !ok {
		t.Fatal("AddressOf returned !ok for an added line")
	}
	if addr.OldLine != -1 || addr.NewLine != 2 {
		t.Errorf("added line address = old %d new %d, want old -1 new 2", addr.OldLine, addr.NewLine)
	}
}

func TestAddressOf_SecondHunkKeepsItsOwnNumbering(t *testing.T) {
	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)

	idx := lineIndexOf(t, parsed, LineAdded, "  persist(data)")
	addr, ok := parsed.AddressOf(idx)
	if !ok {
		t.Fatal("!ok")
	}
	if addr.HunkIndex != 1 {
		t.Errorf("HunkIndex = %d, want 1", addr.HunkIndex)
	}
	if addr.NewLine != 11 {
		t.Errorf("NewLine = %d, want 11", addr.NewLine)
	}
}

func TestAddressOf_OutOfRange(t *testing.T) {
	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)
	for _, idx := range []int{-1, len(parsed.Lines), len(parsed.Lines) + 5} {
		if _, ok := parsed.AddressOf(idx); ok {
			t.Errorf("AddressOf(%d) = ok, want !ok", idx)
		}
	}
}

func TestHunkNavigation(t *testing.T) {
	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)
	firstContent := parsed.Hunks[0].StartLine + 1
	secondContent := parsed.Hunks[1].StartLine + 1

	got, ok := parsed.NextHunkLine(firstContent)
	if !ok || got != secondContent {
		t.Errorf("NextHunkLine from hunk 0 = (%d, %v), want (%d, true)", got, ok, secondContent)
	}

	if _, ok := parsed.NextHunkLine(secondContent); ok {
		t.Error("NextHunkLine from the last hunk should report no next hunk")
	}

	got, ok = parsed.PrevHunkLine(secondContent)
	if !ok || got != firstContent {
		t.Errorf("PrevHunkLine from hunk 1 = (%d, %v), want (%d, true)", got, ok, firstContent)
	}

	if _, ok := parsed.PrevHunkLine(firstContent); ok {
		t.Error("PrevHunkLine from the first hunk should report no previous hunk")
	}
}

func TestFirstCommentableLine_SkipsTheHunkHeader(t *testing.T) {
	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)
	idx := parsed.FirstCommentableLine()
	if idx != parsed.Hunks[0].StartLine+1 {
		t.Errorf("FirstCommentableLine = %d, want %d", idx, parsed.Hunks[0].StartLine+1)
	}
	if parsed.Lines[idx].Type == LineHunkHeader {
		t.Error("first commentable line must not be a hunk header")
	}
}
