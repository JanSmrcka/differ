package ui

import (
	"fmt"
	"strings"

	"github.com/jansmrcka/differ/internal/review"
)

// Building review comments from the diff cursor.
//
// A comment carries everything needed to explain itself later: the file, the
// lines on the side they belong to, and a plain-text excerpt of the change.
// Keeping the excerpt on the comment means feedback can be generated without
// re-reading the repository, and lets a comment outlive the diff it came from.

// maxExcerptChars caps the diff context stored with a comment. Whoever reads
// the feedback needs the change, not the file.
const maxExcerptChars = 1200

// buildLineComment describes the line under the cursor.
func (m Model) buildLineComment() (review.Comment, bool) {
	if m.renderer == nil {
		return review.Comment{}, false
	}
	parsed := m.renderer.Parsed()
	addr, ok := parsed.AddressOf(m.diffCursor)
	if !ok || addr.Type == LineHunkHeader {
		return review.Comment{}, false
	}

	side, line := sideAndLine(addr)
	if line < 0 {
		return review.Comment{}, false
	}

	c := review.Comment{
		File:      m.currentFilePath(),
		Side:      side,
		StartLine: line,
		EndLine:   line,
		HunkIndex: addr.HunkIndex,
		Anchor:    parsed.Lines[m.diffCursor].Content,
	}
	if h, ok := parsed.HunkAt(m.diffCursor); ok {
		c.Excerpt = excerptFor(parsed, h)
	}
	return c, true
}

// buildHunkComment describes the whole hunk the cursor is in.
func (m Model) buildHunkComment() (review.Comment, bool) {
	if m.renderer == nil {
		return review.Comment{}, false
	}
	parsed := m.renderer.Parsed()
	h, ok := parsed.HunkAt(m.diffCursor)
	if !ok {
		return review.Comment{}, false
	}

	start, count := h.NewStart, h.NewCount
	side := review.SideNew
	if count == 0 {
		// A hunk that only deletes has no new-side range.
		start, count, side = h.OldStart, h.OldCount, review.SideOld
	}

	return review.Comment{
		File:      m.currentFilePath(),
		Side:      side,
		StartLine: start,
		EndLine:   start + max(count, 1) - 1,
		HunkIndex: h.Index,
		Anchor:    anchorForHunk(parsed, h),
		Excerpt:   excerptFor(parsed, h),
	}, true
}

// sideAndLine picks which side of the diff a line belongs to. A removed line
// exists only in the old file; everything else is addressed on the new one.
func sideAndLine(addr LineAddress) (review.Side, int) {
	if addr.Type == LineRemoved {
		return review.SideOld, addr.OldLine
	}
	return review.SideNew, addr.NewLine
}

// anchorForHunk is the hunk's first content line, used to re-locate the hunk
// after the diff changes.
func anchorForHunk(parsed ParsedDiff, h Hunk) string {
	for i := h.StartLine; i <= h.LastLine && i < len(parsed.Lines); i++ {
		if parsed.Lines[i].Type != LineHunkHeader {
			return parsed.Lines[i].Content
		}
	}
	return h.Context
}

// excerptFor renders a hunk as plain unified-diff text: no styling, no line
// numbers, just the change with its -/+ markers. Deterministic, so the same
// hunk always produces the same excerpt.
func excerptFor(parsed ParsedDiff, h Hunk) string {
	var b strings.Builder
	total := 0
	for i := h.StartLine; i <= h.LastLine && i < len(parsed.Lines); i++ {
		dl := parsed.Lines[i]
		if dl.Type == LineHunkHeader {
			continue
		}
		line := excerptMarker(dl.Type) + dl.Content
		if total+len(line)+1 > maxExcerptChars {
			fmt.Fprintf(&b, "… truncated (%d more lines)\n", h.LastLine-i+1)
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
		total += len(line) + 1
	}
	return b.String()
}

func excerptMarker(t DiffLineType) string {
	switch t {
	case LineAdded:
		return "+"
	case LineRemoved:
		return "-"
	default:
		return " "
	}
}
