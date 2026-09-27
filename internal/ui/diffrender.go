package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/theme"
)

// DiffRenderer turns a parsed diff into viewport content and tracks where the
// cursor is.
//
// Every line is syntax-highlighted once and cached. Moving the cursor
// re-renders a single line, so navigation stays cheap on diffs that are
// thousands of lines long.
type DiffRenderer struct {
	parsed   ParsedDiff
	filename string
	styles   Styles
	theme    theme.Theme
	width    int

	// lines holds each diff line rendered with an empty gutter. In split mode
	// each entry is a paired row, and rowOf maps a source line index to the
	// row that shows it.
	lines []string
	split bool
	rows  []splitRow
	rowOf []int
}

// NewDiffRenderer renders every line up front.
func NewDiffRenderer(parsed ParsedDiff, filename string, styles Styles, t theme.Theme, width int) *DiffRenderer {
	r := &DiffRenderer{parsed: parsed, filename: filename, styles: styles, theme: t, width: width}
	r.render()
	return r
}

// SetSplit switches between unified and side-by-side rendering. The cursor
// still addresses ParsedDiff.Lines in both, so a comment made in one view
// resolves identically in the other.
func (r *DiffRenderer) SetSplit(split bool) {
	if r.split == split || r.parsed.Binary {
		r.split = split
		return
	}
	r.split = split
	r.render()
}

func (r *DiffRenderer) render() {
	if r.parsed.Binary {
		return
	}
	gutter := blankGutter()
	if !r.split {
		r.rows, r.rowOf = nil, nil
		r.lines = make([]string, len(r.parsed.Lines))
		for i, dl := range r.parsed.Lines {
			r.lines[i] = renderDiffLineGutter(dl, r.filename, r.styles, r.theme, r.width, gutter)
		}
		return
	}

	r.rows = pairLinesIndexed(r.parsed.Lines)
	r.rowOf = make([]int, len(r.parsed.Lines))
	for i := range r.rowOf {
		r.rowOf[i] = -1
	}
	r.lines = make([]string, len(r.rows))
	for i, row := range r.rows {
		if row.leftIdx >= 0 {
			r.rowOf[row.leftIdx] = i
		}
		if row.rightIdx >= 0 {
			r.rowOf[row.rightIdx] = i
		}
		r.lines[i] = r.renderRow(row, gutter)
	}
}

func (r *DiffRenderer) renderRow(row splitRow, gutter string) string {
	if row.left != nil && row.left.Type == LineHunkHeader {
		return renderHunkLine(*row.left, r.styles, r.width, gutter)
	}
	panelW := (r.width - gutterWidth - 1) / 2
	left := renderSplitSide(row.left, r.filename, r.styles, r.theme, panelW, true)
	right := renderSplitSide(row.right, r.filename, r.styles, r.theme, panelW, false)
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color(r.theme.BorderFg)).Render("│")
	return gutter + left + sep + right
}

// Content returns the full diff with the given line marked as current.
// A cursor outside the diff marks nothing.
func (r *DiffRenderer) Content(cursor int) string {
	if r.parsed.Binary {
		return RenderBinaryFile(r.styles, r.width)
	}
	row, ok := r.rowFor(cursor)
	if !ok {
		return strings.Join(r.lines, "\n")
	}

	out := make([]string, len(r.lines))
	copy(out, r.lines)
	if r.split {
		out[row] = r.renderRow(r.rows[row], cursorGutter(r.styles))
	} else {
		out[row] = renderDiffLineGutter(r.parsed.Lines[row], r.filename, r.styles, r.theme, r.width, cursorGutter(r.styles))
	}
	return strings.Join(out, "\n")
}

// rowFor maps a cursor (an index into ParsedDiff.Lines) to the display row
// showing it.
func (r *DiffRenderer) rowFor(cursor int) (int, bool) {
	if cursor < 0 || cursor >= len(r.parsed.Lines) {
		return 0, false
	}
	if !r.split {
		return cursor, true
	}
	if cursor >= len(r.rowOf) || r.rowOf[cursor] < 0 {
		return 0, false
	}
	return r.rowOf[cursor], true
}

// LineCount is the number of addressable lines in the diff — always source
// lines, never display rows, so the cursor means the same thing in both views.
func (r *DiffRenderer) LineCount() int { return len(r.parsed.Lines) }

// DisplayRows is the number of rows Content produces, which differs from
// LineCount in split mode.
func (r *DiffRenderer) DisplayRows() int { return len(r.lines) }

// RowFor exposes the display row for a cursor, for viewport scrolling.
func (r *DiffRenderer) RowFor(cursor int) (int, bool) { return r.rowFor(cursor) }

// Parsed exposes the diff the renderer was built from, so callers can resolve
// the cursor to a line address without re-parsing.
func (r *DiffRenderer) Parsed() ParsedDiff { return r.parsed }
