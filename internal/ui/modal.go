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
// lipgloss does the work — Place centres, RoundedBorder frames. Bubble Tea has
// no modal of its own; there is nothing to add to go.mod for this.

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
	// One ceiling, at the end. Bounding the body here as well was a term no
	// input could reach — the callers fit the body to modalBodyRoom first, so
	// want is never the larger number — and raising it left every screen
	// identical.
	want := len(body) + modalChrome
	room := max(want, modalChrome+1)
	// Never more than half the content area, border included. A box that
	// fills the screen cannot move out of the cursor's way, and the whole
	// claim of a modal over a footer is that you can still see the line you
	// are writing about. Forgetting the border here left a nine-row box in a
	// fifteen-row area, which cannot clear a cursor in the middle of it.
	if half := height/2 - modalBorderRows; room > half && half >= modalChrome+1 {
		room = half
	}
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
		// It would cover the row. Below it if there is room, otherwise above.
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
const commentModalMinHeight = 17

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
	// every covered row at 220 columns, for no reason — and the doc comment
	// above claimed it kept both edges.
	used := lead + lipgloss.Width(box)
	right := ""
	if used < width {
		whole := lipgloss.NewStyle().MaxWidth(used).Render(bottom)
		if tail := strings.TrimPrefix(bottom, whole); tail != "" && lipgloss.Width(whole) == used {
			right = lipgloss.NewStyle().MaxWidth(width - used).Render(tail)
		}
		right = padTo(right, width-used)
	}
	return left + box + right
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
	case m.showAgents:
		// Nothing to avoid: choosing where feedback goes is not judged
		// against a particular line. A list that does not fit is scrolled by
		// agentRows, so it is asked for the room first.
		return m.renderModal(" agent", m.agentRows(m.modalBodyRoom(height)), m.agentClosing(), height, -1)
	case m.commenting:
		// The editor is sized to the room rather than fitted into it:
		// fitOverlay drops what does not fit and says how many rows it
		// dropped, which for a text area means the user types and sees a
		// count instead of their own words.
		return m.renderModal(m.commentTitle(), m.commentRows(m.modalBodyRoom(height)),
			commentClosing, height, m.cursorContentRow())
	}
	return ""
}

// cursorContentRow is the row of the content area the diff cursor is drawn on,
// or -1 when there is none.
//
// The panel starts with a label and a blank line, and the viewport is scrolled,
// so the cursor's line index is not its row on screen.
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

// panelHeaderRows is the label and the blank line under it, which every panel
// draws before its content.
const panelHeaderRows = 2

// modalBodyRoom is how many rows of content the box can hold at this height,
// after the border, the title and the closing line — and after the cap that
// keeps it out of the cursor's way.
func modalBodyRoomAt(height int) int {
	room := max(min(height-2, height), modalChrome+1)
	if half := height/2 - modalBorderRows; half >= modalChrome+1 && room > half {
		room = half
	}
	return max(room-modalChrome, 1)
}

func (m Model) modalBodyRoom(height int) int { return modalBodyRoomAt(height) }
