package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// A modal: a bordered box in the middle of the screen, with the view still
// visible around it.
//
// The chrome has no boxes — that is #48, and two tests sweep every mode for
// box-drawing corners. This does not contradict it: the rule is about the
// frame you look at all day, where a border is decoration that costs a column
// on each side. A modal is transient and asks for an answer, and the border is
// what says the rest of the screen is not taking input. Both no-boxes sweeps
// render the base view, with nothing open.
//
// lipgloss does the work — PlaceHorizontal centres, RoundedBorder frames, and
// placeModal does the vertical placement itself because the box has to keep
// out of the cursor's way rather than sit in the middle. Bubble Tea has no
// modal of its own; there is nothing to add to go.mod for this.

const (
	// modalShare is how much of the terminal a modal asks for.
	modalShare = 70
	// modalMinWidth is the narrowest box worth drawing. Below it the border
	// costs more than it explains and the content goes full width instead.
	modalMinWidth = 34
	// modalMaxWidth stops a modal spanning a very wide terminal, which reads
	// as a second screen rather than a box on this one.
	modalMaxWidth = 88
	// modalPadding is one column inside the border, either side.
	modalPadding = 1
)

// modalWidth is the box's outer width, border included.
//
// It has to actually be that. lipgloss.Style.Width sizes the padded content
// block and draws the border outside it, so rendering at modalWidth() made
// every box two columns wider than the number said — modalMinWidth of 34 was
// 36 on screen and modalMaxWidth of 88 was 90. modalBoxWidth is what the
// style is given.
func (m Model) modalWidth() int {
	want := m.width * modalShare / 100
	return min(max(want, modalMinWidth), min(modalMaxWidth, m.width))
}

// modalBoxWidth is what lipgloss is asked for: the outer width less the two
// border columns it adds itself.
func (m Model) modalBoxWidth() int { return max(m.modalWidth()-2, 1) }

// renderModal draws a titled box over height rows, away from avoid.
//
// The content is fitted by fitOverlay, which already knows how to drop rows and
// keep the closing line, so a modal cannot outgrow the room it is given.
//
// avoid is a content row the box must not cover — the line a comment is being
// written about. A centred box and a cursor the diff keeps near the centre
// collide by construction, so the box goes to whichever half has more room.
// Negative means nothing to avoid.
func (m Model) renderModal(title string, body []string, closing string, height, avoid int) string {
	// The border and the padding are not content.
	inner := m.modalBoxWidth() - 2*modalPadding
	if inner < 1 {
		inner = 1
	}
	// At least one row of body, always. Without a floor the box below 13 rows
	// was all chrome: you typed into a comment and nothing appeared, and at 8
	// and 9 the closing line went too, so nothing said how to get out. The
	// footer editor this replaced guaranteed a row, and minHeight is 8.
	// One expression, and no floor of its own: boxRows already guarantees a
	// row, and a `max(want, modalChrome+1)` here was inert — with an empty
	// body it bought one blank row nothing on screen depends on, and
	// removing it changed no size and no test.
	//
	// boxRows keeps the box to half the content area, border included. A box
	// that fills the screen cannot move out of the cursor's way, and the
	// whole claim of a modal over a footer is that you can still see the
	// line you are writing about.
	room := min(len(body)+modalChrome, boxRows(height, avoid))
	// The ceiling last, so it wins over the floor. With the floor applied
	// afterwards a height too small for a whole box produced one too tall for
	// it, and placeModal then cut the bottom border off: at six rows the box
	// had a top, a title and no way out drawn. A cramped box is still a box;
	// a box with one side missing is not.
	room = min(room, max(height-modalBorderRows, 1))

	content := m.fitOverlay(title, body, closing, inner, room)
	box := m.styles.Modal.Width(m.modalBoxWidth()).Render(content)
	return placeModal(box, m.width, height, avoid)
}

// placeModal centres the box horizontally and puts it in whichever half of the
// screen does not hold avoid.
func placeModal(box string, width, height, avoid int) string {
	tall := lipgloss.Height(box)
	top := max((height-tall)/2, 0)

	if avoid >= 0 && avoid >= top && avoid < top+tall {
		// It would cover the row. Below it if there is room, otherwise above,
		// ending on the row before it.
		//
		// The clamp is a floor, not a placement decision. It is reached only
		// by a box too tall to fit on either side, and such a box covers the
		// line wherever it is put — so nothing here can prevent that case,
		// and nothing here pretends to. What prevents it is
		// commentModalMinHeight, which sizes the content area to hold twice
		// the box; the comment modal is the only caller that passes an avoid
		// at all, so that is the whole of the guarantee.
		if below := avoid + 1; below+tall <= height {
			top = below
		} else {
			top = max(avoid-tall, 0)
		}
	}

	rows := make([]string, 0, height)
	for i := 0; i < top; i++ {
		rows = append(rows, "")
	}
	rows = append(rows, strings.Split(lipgloss.PlaceHorizontal(width, lipgloss.Center, box), "\n")...)
	for len(rows) < height {
		rows = append(rows, "")
	}
	return strings.Join(rows[:height], "\n")
}

// commentModalMinHeight is the smallest terminal a comment modal is drawn in.
// Below it the editor goes back to the footer, which needs two rows.
//
// Measured against the terminal rather than the content area on purpose:
// contentHeight asks how tall the footer is, and the footer asks whether the
// editor is in it — which is this decision. The first version recursed until
// the stack ran out.
//
// The number is geometry, not taste. The box must fit entirely above or
// entirely below the line it is about, and the worst case is a cursor sitting
// one row too low to clear it above — which needs twice the box's height in
// the content area. The smallest box is one body row inside modalChrome and a
// border, so 2*(modalChrome+1+modalBorderRows) = 14 rows of content, and the
// frame spends three on chrome and two on the footer: 19.
//
// It was 17, which held only while the panels drew a blank row under their
// label and pushed the cursor one row further down. When that row went, a
// cursor in the middle of a 12-row content area fitted on neither side and
// placeModal put the box over the line — the one thing it exists to avoid.
const commentModalMinHeight = 19

// modalBorderRows is the top and bottom of the box, which are not content.
const modalBorderRows = 2

// modalChrome is what fitOverlay spends on the title, the blank lines and the
// closing line.
const modalChrome = 4

// modalOver draws a modal on top of the rows behind it, so the view is still
// visible around the box rather than blanked.
func modalOver(behind []string, modal string, width, height int) string {
	rows := padLines(behind, height)
	over := strings.Split(modal, "\n")
	for i := range rows {
		if i >= len(over) {
			break
		}
		// lipgloss.Place pads every row to the full width, so a row of the
		// modal is either all whitespace — where the view shows through — or
		// the box itself.
		if strings.TrimSpace(over[i]) == "" {
			continue
		}
		rows[i] = overlayRow(rows[i], over[i], width)
	}
	return strings.Join(rows, "\n")
}

// overlayRow puts the non-blank middle of top over bottom, keeping what is
// left and right of it.
//
// Written with lipgloss widths rather than byte offsets: the rows underneath
// are full of escapes and multi-byte glyphs, and slicing them by index cuts
// both.
func overlayRow(bottom, top string, width int) string {
	trimmed := strings.TrimRight(top, " ")
	lead := lipgloss.Width(trimmed) - lipgloss.Width(strings.TrimLeft(trimmed, " "))
	if lead < 0 {
		lead = 0
	}
	box := strings.TrimLeft(trimmed, " ")
	// MaxWidth, not truncateEnd: the row underneath is only being covered, not
	// shortened, so it must be cut silently. truncateEnd appends the
	// truncation marker, which put a "…" against the left edge of every modal
	// row. MaxWidth also understands the escapes the row is full of.
	//
	// MaxWidth(0) does not truncate — it returns the string whole — so a box
	// flush against the left edge would emit the row underneath as well and
	// wrap the frame.
	left := ""
	if lead > 0 {
		left = padTo(lipgloss.NewStyle().MaxWidth(lead).Render(bottom), lead)
	}

	// What is right of the box stays. Blanking it wiped 67 columns of diff on
	// every covered row at 220 columns, for no reason.
	//
	// Cut with dropColumns, not by measuring MaxWidth's output and trimming
	// that prefix off. MaxWidth re-emits the string with its own escapes and
	// a reset, so the prefix it returns is not a byte prefix of the original
	// whenever the row has more than one styled run — which is every real
	// row: line numbers, marker, syntax colours. TrimPrefix then stripped
	// nothing, the tail was the whole row, and what appeared right of the box
	// was the row's *beginning* repeated. That reads as real diff, which is
	// worse than the blank it replaced.
	used := lead + lipgloss.Width(box)
	right := ""
	if used < width {
		right = clipRow(dropColumns(bottom, used), width-used)
		right = padTo(right, width-used)
	}
	return left + box + right
}

// dropColumns returns row without its first n display columns, keeping the
// styling that was in force at the cut.
//
// The escapes are not columns, so they are collected as the scan passes them
// and re-emitted in front of the tail; without that the tail would be printed
// in whatever colour the previous row left behind. The cut itself is made on
// the visible text alone, by lipgloss, so a grapheme cluster is never split —
// an underline between the runes of a ZWJ emoji is the trap that made
// lipgloss.Width measure one glyph as two.
func dropColumns(row string, n int) string {
	if n <= 0 {
		return row
	}
	plain, spans := splitANSI(row)
	kept := lipgloss.NewStyle().MaxWidth(n).Render(plain)
	skip := len(kept)
	if lipgloss.Width(plain) <= n {
		return ""
	}

	var style, out strings.Builder
	seen := 0
	for _, sp := range spans {
		if sp.escape {
			if seen < skip {
				style.WriteString(sp.text)
			} else {
				out.WriteString(sp.text)
			}
			continue
		}
		if seen+len(sp.text) <= skip {
			seen += len(sp.text)
			continue
		}
		if seen < skip {
			out.WriteString(sp.text[skip-seen:])
			seen = skip
			continue
		}
		out.WriteString(sp.text)
	}
	return style.String() + out.String()
}

// rowSpan is one run of a row: either an escape sequence, which occupies no
// columns, or visible text. Named for the row rather than `span`, which
// intraline.go already uses for a run of emphasis.
type rowSpan struct {
	text   string
	escape bool
}

// splitANSI separates a row into its escape sequences and its visible text,
// returning the visible text on its own as well so it can be measured and cut
// without them.
func splitANSI(row string) (string, []rowSpan) {
	var plain strings.Builder
	var spans []rowSpan
	for i := 0; i < len(row); {
		if row[i] == 0x1b {
			j := escapeEnd(row, i)
			spans = append(spans, rowSpan{text: row[i:j], escape: true})
			i = j
			continue
		}
		j := i
		for j < len(row) && row[j] != 0x1b {
			j++
		}
		spans = append(spans, rowSpan{text: row[i:j]})
		plain.WriteString(row[i:j])
		i = j
	}
	return plain.String(), spans
}

// escapeEnd is the index just past the escape sequence starting at i.
//
// Everything this package emits comes from lipgloss or chroma and is a CSI
// sequence ending in a letter; anything else is consumed as two bytes so the
// scan cannot stall.
func escapeEnd(row string, i int) int {
	if i+1 >= len(row) {
		return len(row)
	}
	if row[i+1] != '[' {
		return i + 2
	}
	for j := i + 2; j < len(row); j++ {
		if c := row[j]; (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return j + 1
		}
	}
	return len(row)
}

// modal is the box on top, or "" when nothing is asking.
//
// Only one at a time: they are all questions, and two questions at once is a
// screen nobody can answer.
func (m Model) modal(height int) string {
	// Below this the box is all border and chrome: there is no room for a row
	// of what you are typing, let alone for the diff around it. The footer
	// editor needs two rows and keeps working, so that is what small
	// terminals get.
	if m.commenting && !m.showAgents && m.height < commentModalMinHeight {
		return ""
	}
	switch {
	case m.mode == modeBranchPicker:
		// Nothing to avoid: which branch to check out is not a question about
		// a particular line. The list is scrolled by branchRows, so it is
		// asked for the room first.
		return m.renderModal(m.branchTitle(), m.branchRows(m.modalBodyRoom(height, -1)),
			m.branchClosing(), height, -1)
	case m.showAgents:
		// Nothing to avoid: choosing where feedback goes is not judged
		// against a particular line. A list that does not fit is scrolled by
		// agentRows, so it is asked for the room first.
		return m.renderModal(" agent", m.agentRows(m.modalBodyRoom(height, -1)), m.agentClosing(), height, -1)
	case m.commenting:
		// The editor is sized to the room rather than fitted into it:
		// fitOverlay drops what does not fit and says how many rows it
		// dropped, which for a text area means the user types and sees a
		// count instead of their own words.
		return m.renderModal(m.commentTitle(), m.commentRows(m.modalBodyRoom(height, m.cursorContentRow())),
			commentClosing, height, m.cursorContentRow())
	}
	return ""
}

// cursorContentRow is the row of the content area the diff cursor is drawn on,
// or -1 when there is none.
//
// The panel starts with its one label row, and the viewport is scrolled, so
// the cursor's line index is not its row on screen.
func (m Model) cursorContentRow() int {
	if m.renderer == nil {
		return -1
	}
	at, ok := m.renderer.RowFor(m.diffCursor)
	if !ok {
		return -1
	}
	row := at - m.viewport.YOffset + panelHeaderRows
	if row < 0 || row >= m.contentHeight() {
		return -1
	}
	return row
}

// panelHeaderRows is the one label row every panel
// draws before its content.
const panelHeaderRows = 1

// modalBodyRoom is how many rows of content the box can hold at this height,
// after the border, the title and the closing line — and after the cap that
// keeps it out of the cursor's way.
func modalBodyRoomAt(height, avoid int) int {
	return max(boxRows(height, avoid)-modalChrome, 1)
}

// boxRows is how many rows of the box are content: never more than half the
// area, never fewer than one row of body, and never more than the area.
//
// One function, used both to size the body and to draw it. They were two
// expressions that disagreed: the drawn box was capped to half the area and
// the body was not, so at short heights fitOverlay was handed more rows than
// the box would show and replaced the overflow with a count — the picker's
// highlighted row among them.
//
// The half cap applies at every height a box has something to avoid. Its
// guard used to be `half >= modalChrome+1`, which switched it off entirely
// below a content height of fourteen, so at terminal heights 17 and 18 — the
// first two the box is drawn at — it took the whole area and covered the line
// being commented on.
//
// With nothing to avoid it does not apply at all: its whole justification is
// that the box must be able to move out of the cursor's way, and a picker is
// not judged against a particular line. Capping those cost the branch picker
// half its rows — at fourteen it had room for the filter and not one branch,
// so there was nothing to pick from.
func boxRows(height, avoid int) int {
	limit := height - modalBorderRows
	if avoid >= 0 {
		limit = min(limit, max(height/2-modalBorderRows, modalChrome+1))
	}
	return max(limit, 1)
}

func (m Model) modalBodyRoom(height, avoid int) int {
	return modalBodyRoomAt(height, avoid)
}
