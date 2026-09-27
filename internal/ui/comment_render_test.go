package ui

import (
	"strings"
	"testing"

	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/testutil"
)

func rendererWithComments(t *testing.T, cs []review.Comment) (*DiffRenderer, ParsedDiff) {
	t.Helper()
	styles, th := testStyles()
	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)
	r := NewDiffRenderer(parsed, "src.ts", styles, th, 100)
	r.SetComments(cs)
	return r, parsed
}

func TestRenderComments_BodyAppearsUnderItsAnchor(t *testing.T) {
	r, parsed := rendererWithComments(t, []review.Comment{
		{ID: "c1", File: "src.ts", Side: review.SideNew, StartLine: 2, EndLine: 2, Body: "keep this awaited", State: review.StatePending},
	})

	out := strings.Split(r.Content(-1), "\n")
	anchor := lineIndexOf(t, parsed, LineAdded, "  const user = await getUser(id)")
	row, ok := r.RowFor(anchor)
	if !ok {
		t.Fatal("anchor has no row")
	}

	if !strings.Contains(out[row], "await getUser(id)") {
		t.Fatalf("row %d is not the anchor line: %q", row, out[row])
	}
	if !strings.Contains(out[row+1], "line 2") || !strings.Contains(out[row+1], "pending") {
		t.Errorf("comment header should follow its anchor, got %q", out[row+1])
	}
	if !strings.Contains(out[row+2], "keep this awaited") {
		t.Errorf("comment body should follow its header, got %q", out[row+2])
	}
}

func TestRenderComments_AddRowsSoLaterLinesShiftDown(t *testing.T) {
	plain, _ := rendererWithComments(t, nil)
	withComment, _ := rendererWithComments(t, []review.Comment{
		{ID: "c1", File: "src.ts", Side: review.SideNew, StartLine: 2, EndLine: 2, Body: "one\ntwo", State: review.StatePending},
	})

	if withComment.DisplayRows() <= plain.DisplayRows() {
		t.Errorf("comment rows not added: %d vs %d", withComment.DisplayRows(), plain.DisplayRows())
	}
	// Addressing must not change — only the display grows.
	if withComment.LineCount() != plain.LineCount() {
		t.Errorf("LineCount changed from %d to %d", plain.LineCount(), withComment.LineCount())
	}
}

func TestRenderComments_RowForAccountsForInsertedRows(t *testing.T) {
	r, parsed := rendererWithComments(t, []review.Comment{
		{ID: "c1", File: "src.ts", Side: review.SideNew, StartLine: 2, EndLine: 2, Body: "note", State: review.StatePending},
	})

	out := strings.Split(r.Content(-1), "\n")
	// Every source line must still map to the row that actually shows it.
	for i := 0; i < r.LineCount(); i++ {
		row, ok := r.RowFor(i)
		if !ok {
			continue
		}
		if row >= len(out) {
			t.Fatalf("line %d maps to row %d, past the end (%d rows)", i, row, len(out))
		}
		content := parsed.Lines[i].Content
		if strings.TrimSpace(content) == "" {
			continue
		}
		if !strings.Contains(out[row], strings.TrimSpace(content)) {
			t.Errorf("line %d (%q) maps to row %d which shows %q", i, content, row, out[row])
		}
	}
}

func TestRenderComments_MarkerInGutterOnCommentedLine(t *testing.T) {
	r, parsed := rendererWithComments(t, []review.Comment{
		{ID: "c1", File: "src.ts", Side: review.SideNew, StartLine: 2, EndLine: 2, Body: "note", State: review.StatePending},
	})

	out := strings.Split(r.Content(-1), "\n")
	anchor := lineIndexOf(t, parsed, LineAdded, "  const user = await getUser(id)")
	row, _ := r.RowFor(anchor)

	if !strings.Contains(out[row], commentMarker) {
		t.Errorf("commented line should carry a gutter marker: %q", out[row])
	}
}

func TestRenderComments_PendingAndSentAreDistinguishable(t *testing.T) {
	r, _ := rendererWithComments(t, []review.Comment{
		{ID: "c1", File: "src.ts", Side: review.SideNew, StartLine: 2, EndLine: 2, Body: "pending one", State: review.StatePending},
		{ID: "c2", File: "src.ts", Side: review.SideNew, StartLine: 11, EndLine: 11, Body: "sent one", State: review.StateSent},
	})

	out := r.Content(-1)
	if !strings.Contains(out, "pending") {
		t.Error("pending comment should be labelled")
	}
	if !strings.Contains(out, "sent") {
		t.Error("sent comment should be labelled")
	}
}

func TestRenderComments_MultilineBodyRendersEveryLine(t *testing.T) {
	r, _ := rendererWithComments(t, []review.Comment{
		{ID: "c1", File: "src.ts", Side: review.SideNew, StartLine: 2, EndLine: 2, Body: "alpha\nbeta\ngamma", State: review.StatePending},
	})

	out := r.Content(-1)
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(out, want) {
			t.Errorf("comment body line %q missing", want)
		}
	}
}

func TestRenderComments_CursorStillMarksTheRightRow(t *testing.T) {
	r, parsed := rendererWithComments(t, []review.Comment{
		{ID: "c1", File: "src.ts", Side: review.SideNew, StartLine: 2, EndLine: 2, Body: "note", State: review.StatePending},
	})

	// A line after the comment: its marker must land on its own row.
	target := lineIndexOf(t, parsed, LineContext, "  return user")
	row, _ := r.RowFor(target)
	out := strings.Split(r.Content(target), "\n")

	if !strings.Contains(out[row], cursorMarker) {
		t.Errorf("cursor marker is not on row %d: %q", row, out[row])
	}
	if strings.Count(r.Content(target), cursorMarker) != 1 {
		t.Error("exactly one row should carry the cursor")
	}
}

func TestRenderComments_WorkInSplitView(t *testing.T) {
	styles, th := testStyles()
	parsed := ParseDiff(testutil.Fixture(t, "multi_hunk").Diff)
	r := NewDiffRenderer(parsed, "src.ts", styles, th, 120)
	r.SetSplit(true)
	r.SetComments([]review.Comment{
		{ID: "c1", File: "src.ts", Side: review.SideNew, StartLine: 2, EndLine: 2, Body: "split note", State: review.StatePending},
	})

	if !strings.Contains(r.Content(-1), "split note") {
		t.Error("comment body missing in split view")
	}
}

func TestRenderComments_HunkCommentAnchorsToItsFirstLine(t *testing.T) {
	r, parsed := rendererWithComments(t, []review.Comment{
		{ID: "c1", File: "src.ts", Side: review.SideNew, StartLine: 1, EndLine: 5, Body: "whole hunk", State: review.StatePending},
	})

	out := strings.Split(r.Content(-1), "\n")
	row, ok := r.RowFor(parsed.Hunks[0].StartLine + 1)
	if !ok {
		t.Fatal("no row for the hunk's first line")
	}
	if !strings.Contains(out[row+1], "1-5") {
		t.Errorf("a range comment should show its range, got %q", out[row+1])
	}
	if !strings.Contains(out[row+2], "whole hunk") {
		t.Errorf("hunk comment body should follow its header, got %q", out[row+2])
	}
}
