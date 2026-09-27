package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/theme"
)

// DiffRenderer turns a parsed diff into viewport content.
//
// Two indexes are in play and must not be confused:
//
//   - a *line index* addresses ParsedDiff.Lines. It is what the cursor holds
//     and what review comments anchor to, and it means the same thing in
//     unified and split view.
//   - a *display row* is a line of output. Split view pairs two source lines
//     onto one row, and an inline comment adds rows of its own, so the two
//     indexes drift apart. RowFor maps between them.
//
// Every line is syntax-highlighted once and cached, so moving the cursor
// re-renders a single row rather than the whole diff.
type DiffRenderer struct {
	parsed   ParsedDiff
	filename string
	styles   Styles
	theme    theme.Theme
	width    int

	split    bool
	tabWidth int
	comments []review.Comment

	// dirty defers re-rendering until the content is actually asked for, so
	// several setters cost one render.
	dirty bool

	pairs []splitRow
	rows  []displayRow
	// rowOf maps a source line index to the display row showing it, or -1.
	rowOf []int
}

// displayRow is one rendered line of output, remembering what produced it so
// it can be re-rendered when the cursor lands on it.
type displayRow struct {
	text string
	// line is the source line index for a unified row, pair the index into
	// pairs for a split row. Both are -1 for a comment row.
	line int
	pair int
}

// NewDiffRenderer renders every line up front.
func NewDiffRenderer(parsed ParsedDiff, filename string, styles Styles, t theme.Theme, width int) *DiffRenderer {
	// Without this the Chroma style stays nil and every line renders
	// unhighlighted.
	initChromaStyle(t.ChromaStyle)

	r := &DiffRenderer{
		parsed: parsed, filename: filename, styles: styles,
		theme: t, width: width, tabWidth: defaultTabWidth,
	}
	r.render()
	return r
}

// SetTabWidth sets how wide a tab renders. Zero or less keeps the default.
func (r *DiffRenderer) SetTabWidth(n int) {
	if n <= 0 || n == r.tabWidth {
		return
	}
	r.tabWidth = n
	r.dirty = true
}

// ensure re-renders if a setter changed something since the last render.
func (r *DiffRenderer) ensure() {
	if r.dirty {
		r.render()
		r.dirty = false
	}
}

// displayLine returns a copy of a source line with tabs expanded for display.
// The original keeps its tabs so comment anchors and excerpts show the file as
// it really is.
func (r *DiffRenderer) displayLine(dl DiffLine) DiffLine {
	dl.Content = expandTabs(dl.Content, r.tabWidth)
	return dl
}

// SetSplit switches between unified and side-by-side rendering.
func (r *DiffRenderer) SetSplit(split bool) {
	if r.split == split {
		return
	}
	r.split = split
	r.dirty = true
}

// SetComments attaches review comments for inline display. The caller passes
// only the comments belonging to this file.
func (r *DiffRenderer) SetComments(cs []review.Comment) {
	r.comments = cs
	r.dirty = true
}

func (r *DiffRenderer) render() {
	if r.parsed.Binary {
		r.rows, r.rowOf = nil, nil
		return
	}

	r.rowOf = make([]int, len(r.parsed.Lines))
	for i := range r.rowOf {
		r.rowOf[i] = -1
	}
	byLine := r.commentsByLine()
	r.rows = r.rows[:0]

	if !r.split {
		r.pairs = nil
		for i, dl := range r.parsed.Lines {
			r.rowOf[i] = len(r.rows)
			r.rows = append(r.rows, displayRow{
				text: renderDiffLineGutter(r.displayLine(dl), r.filename, r.styles, r.theme, r.width, r.gutterFor(byLine, i)),
				line: i, pair: -1,
			})
			r.appendCommentRows(byLine[i])
		}
		return
	}

	r.pairs = pairLinesIndexed(r.parsed.Lines)
	for pi, row := range r.pairs {
		gutter := blankGutter()
		if r.hasComment(byLine, row.leftIdx) || r.hasComment(byLine, row.rightIdx) {
			gutter = commentGutter(r.styles)
		}
		idx := len(r.rows)
		r.rows = append(r.rows, displayRow{text: r.renderRow(row, gutter), line: -1, pair: pi})
		if row.leftIdx >= 0 {
			r.rowOf[row.leftIdx] = idx
		}
		if row.rightIdx >= 0 {
			r.rowOf[row.rightIdx] = idx
		}
		r.appendCommentRows(byLine[row.leftIdx])
		if row.rightIdx != row.leftIdx {
			r.appendCommentRows(byLine[row.rightIdx])
		}
	}
}

func (r *DiffRenderer) hasComment(byLine map[int][]review.Comment, idx int) bool {
	return idx >= 0 && len(byLine[idx]) > 0
}

func (r *DiffRenderer) appendCommentRows(cs []review.Comment) {
	for _, c := range cs {
		for _, text := range r.renderComment(c) {
			r.rows = append(r.rows, displayRow{text: text, line: -1, pair: -1})
		}
	}
}

// commentsByLine resolves each comment to the source line it anchors to.
func (r *DiffRenderer) commentsByLine() map[int][]review.Comment {
	out := map[int][]review.Comment{}
	if len(r.comments) == 0 {
		return out
	}
	for _, c := range r.comments {
		if idx, ok := r.anchorIndex(c); ok {
			out[idx] = append(out[idx], c)
		}
	}
	return out
}

// anchorIndex finds the line a comment hangs off: the first line whose side
// and number match the comment's start.
func (r *DiffRenderer) anchorIndex(c review.Comment) (int, bool) {
	for i := range r.parsed.Lines {
		addr, ok := r.parsed.AddressOf(i)
		if !ok || addr.Type == LineHunkHeader {
			continue
		}
		side, line := sideAndLine(addr)
		if side == c.Side && line == c.StartLine {
			return i, true
		}
	}
	return 0, false
}

// renderComment lays a comment out as indented rows under its anchor.
func (r *DiffRenderer) renderComment(c review.Comment) []string {
	indent := strings.Repeat(" ", gutterWidth+lineNumWidth*2+1)
	bar := r.styles.CommentBar.Render(commentBar)

	label := fmt.Sprintf("line %d", c.StartLine)
	if c.EndLine > c.StartLine {
		label = fmt.Sprintf("lines %d-%d", c.StartLine, c.EndLine)
	}
	header := fmt.Sprintf("%s · %s", label, c.State)

	rows := []string{indent + bar + " " + r.styles.CommentMeta.Render(header)}
	for _, line := range strings.Split(c.Body, "\n") {
		rows = append(rows, indent+bar+" "+r.styles.CommentBody.Render(line))
	}
	return rows
}

func (r *DiffRenderer) gutterFor(byLine map[int][]review.Comment, idx int) string {
	if r.hasComment(byLine, idx) {
		return commentGutter(r.styles)
	}
	return blankGutter()
}

func (r *DiffRenderer) renderRow(row splitRow, gutter string) string {
	if row.left != nil && row.left.Type == LineHunkHeader {
		return renderHunkLine(r.displayLine(*row.left), r.styles, r.width, gutter)
	}
	panelW := (r.width - gutterWidth - 1) / 2
	left := renderSplitSide(r.displaySide(row.left), r.filename, r.styles, r.theme, panelW, true)
	right := renderSplitSide(r.displaySide(row.right), r.filename, r.styles, r.theme, panelW, false)
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color(r.theme.BorderFg)).Render("│")
	return gutter + left + sep + right
}

// displaySide expands tabs on one side of a split row, preserving nil.
func (r *DiffRenderer) displaySide(dl *DiffLine) *DiffLine {
	if dl == nil {
		return nil
	}
	out := r.displayLine(*dl)
	return &out
}

// Content returns the diff with the given line marked as current. A cursor
// outside the diff marks nothing.
func (r *DiffRenderer) Content(cursor int) string {
	r.ensure()
	if r.parsed.Binary {
		return RenderBinaryFile(r.styles, r.width)
	}

	texts := make([]string, len(r.rows))
	for i, row := range r.rows {
		texts[i] = row.text
	}
	if row, ok := r.rowFor(cursor); ok {
		texts[row] = r.renderCursorRow(r.rows[row])
	}
	return strings.Join(texts, "\n")
}

func (r *DiffRenderer) renderCursorRow(row displayRow) string {
	gutter := cursorGutter(r.styles)
	switch {
	case row.pair >= 0:
		return r.renderRow(r.pairs[row.pair], gutter)
	case row.line >= 0:
		return renderDiffLineGutter(r.displayLine(r.parsed.Lines[row.line]), r.filename, r.styles, r.theme, r.width, gutter)
	default:
		return row.text
	}
}

func (r *DiffRenderer) rowFor(cursor int) (int, bool) {
	r.ensure()
	if cursor < 0 || cursor >= len(r.rowOf) || r.rowOf[cursor] < 0 {
		return 0, false
	}
	return r.rowOf[cursor], true
}

// LineCount is the number of addressable source lines — never display rows, so
// the cursor means the same thing in both views and with comments shown.
func (r *DiffRenderer) LineCount() int { return len(r.parsed.Lines) }

// DisplayRows is how many rows Content produces.
func (r *DiffRenderer) DisplayRows() int { r.ensure(); return len(r.rows) }

// RowFor maps a line index to the display row showing it.
func (r *DiffRenderer) RowFor(cursor int) (int, bool) { return r.rowFor(cursor) }

// Parsed exposes the diff the renderer was built from.
func (r *DiffRenderer) Parsed() ParsedDiff { return r.parsed }
