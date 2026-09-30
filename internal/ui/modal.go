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
func (m Model) modalWidth() int {
	want := m.width * modalShare / 100
	return min(max(want, modalMinWidth), min(modalMaxWidth, m.width))
}

// renderModal draws a titled box, centred over height rows.
//
// The content is fitted by fitOverlay, which already knows how to drop rows and
// keep the closing line, so a modal cannot outgrow the room it is given.
func (m Model) renderModal(title string, body []string, closing string, height int) string {
	// The border and the padding are not content.
	inner := m.modalWidth() - 2*modalPadding - 2
	if inner < 1 {
		inner = 1
	}
	// Two fewer rows than the screen, so the box never touches the rules.
	room := max(min(height-2, len(body)+modalChrome), modalChrome)

	content := m.fitOverlay(title, body, closing, inner, room)
	box := m.styles.Modal.Width(inner).Render(content)
	return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, box)
}

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

// overlayRow puts the non-blank middle of top over bottom, keeping bottom's
// left and right edges.
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
	left := lipgloss.NewStyle().MaxWidth(lead).Render(bottom)
	// Pad in case the row underneath was shorter than the modal's left edge.
	left = padTo(left, lead)
	right := ""
	if used := lead + lipgloss.Width(box); used < width {
		right = strings.Repeat(" ", width-used)
	}
	return left + box + right
}

// modal is the box on top, or "" when nothing is asking.
//
// Only one at a time: they are all questions, and two questions at once is a
// screen nobody can answer.
func (m Model) modal(height int) string {
	switch {
	case m.showAgents:
		return m.renderModal(" agent", m.agentRows(), m.agentClosing(), height)
	case m.commenting:
		return m.renderModal(m.commentTitle(), m.commentRows(), commentClosing, height)
	}
	return ""
}
