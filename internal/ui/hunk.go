package ui

import (
	"fmt"
	"strings"
)

// Addressing and navigation over a parsed diff.
//
// Review comments anchor to a position in the diff, so every lookup here has
// to agree with what the renderer puts on screen: an index into
// ParsedDiff.Lines is the single currency for "where the user is".

// LineAddress identifies a position in a diff in terms a reviewer (and a
// coding agent) can act on: which hunk, and which line in the old and new
// versions of the file.
type LineAddress struct {
	HunkIndex int
	// OldLine and NewLine are 1-based file line numbers, or -1 when the line
	// does not exist on that side.
	OldLine int
	NewLine int
	Type    DiffLineType
}

// AddressOf resolves an index into Lines. It reports false for an index
// outside the diff.
func (p ParsedDiff) AddressOf(lineIdx int) (LineAddress, bool) {
	if lineIdx < 0 || lineIdx >= len(p.Lines) {
		return LineAddress{}, false
	}
	l := p.Lines[lineIdx]
	addr := LineAddress{HunkIndex: -1, OldLine: l.OldNum, NewLine: l.NewNum, Type: l.Type}
	if h, ok := p.HunkAt(lineIdx); ok {
		addr.HunkIndex = h.Index
	}
	return addr, true
}

// HunkAt returns the hunk containing the given line index.
func (p ParsedDiff) HunkAt(lineIdx int) (Hunk, bool) {
	for _, h := range p.Hunks {
		if lineIdx >= h.StartLine && lineIdx <= h.LastLine {
			return h, true
		}
	}
	return Hunk{}, false
}

// NextHunkLine returns the first commentable line of the hunk after the one
// containing from. It reports false when there is no later hunk.
func (p ParsedDiff) NextHunkLine(from int) (int, bool) {
	h, ok := p.HunkAt(from)
	if !ok {
		// Not inside a hunk: fall back to the first hunk after this point.
		for _, c := range p.Hunks {
			if c.StartLine > from {
				return p.firstLineOf(c), true
			}
		}
		return 0, false
	}
	if h.Index+1 >= len(p.Hunks) {
		return 0, false
	}
	return p.firstLineOf(p.Hunks[h.Index+1]), true
}

// PrevHunkLine returns the first commentable line of the hunk before the one
// containing from. It reports false when there is no earlier hunk.
func (p ParsedDiff) PrevHunkLine(from int) (int, bool) {
	h, ok := p.HunkAt(from)
	if !ok {
		for i := len(p.Hunks) - 1; i >= 0; i-- {
			if p.Hunks[i].LastLine < from {
				return p.firstLineOf(p.Hunks[i]), true
			}
		}
		return 0, false
	}
	if h.Index == 0 {
		return 0, false
	}
	return p.firstLineOf(p.Hunks[h.Index-1]), true
}

// FirstCommentableLine returns where a cursor should start: the first line a
// review comment can attach to, skipping hunk headers.
func (p ParsedDiff) FirstCommentableLine() int {
	for i, l := range p.Lines {
		if l.Type != LineHunkHeader && l.Type != LineFileHeader {
			return i
		}
	}
	return 0
}

// firstLineOf is the first line of a hunk a comment can attach to: past the
// @@ header where there is one, otherwise the hunk's first line.
func (p ParsedDiff) firstLineOf(h Hunk) int {
	if h.StartLine < 0 || h.StartLine >= len(p.Lines) {
		return 0
	}
	if p.Lines[h.StartLine].Type == LineHunkHeader && h.StartLine+1 <= h.LastLine {
		return h.StartLine + 1
	}
	return h.StartLine
}

// ParseNewFile represents an untracked file's contents as an all-added diff,
// so new files are addressable — and reviewable — exactly like modified ones.
func ParseNewFile(content string) ParsedDiff {
	raw := strings.Split(content, "\n")
	// A trailing newline yields an empty final element that is not a line.
	if n := len(raw); n > 0 && raw[n-1] == "" {
		raw = raw[:n-1]
	}

	lines := make([]DiffLine, 0, len(raw))
	for i, l := range raw {
		if len(lines) >= maxDiffLines {
			lines = append(lines, DiffLine{
				Type: LineHunkHeader, Content: fmt.Sprintf("… truncated (%d+ lines)", maxDiffLines),
				OldNum: -1, NewNum: -1,
			})
			break
		}
		lines = append(lines, DiffLine{Type: LineAdded, Content: l, OldNum: -1, NewNum: i + 1})
	}
	if len(lines) == 0 {
		return ParsedDiff{}
	}
	return ParsedDiff{
		Lines: lines,
		Hunks: []Hunk{{
			Index:     0,
			OldStart:  0,
			OldCount:  0,
			NewStart:  1,
			NewCount:  len(lines),
			StartLine: 0,
			LastLine:  len(lines) - 1,
		}},
	}
}
