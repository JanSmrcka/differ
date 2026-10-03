package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The branch picker as a box over the view.
//
// It used to replace the file list panel, so choosing a branch cost you sight
// of the changeset you were looking at — and the new-branch prompt was a
// footer bar, a third shape for the same kind of question. The comment editor
// and the agent picker are boxes; this is the same question and gets the same
// answer.
//
// modeBranchPicker stays. The picker needs a mode for key routing, and
// keymap_test.go's checks — handled but undocumented, bound twice, every mode
// offers an exit — are all keyed on one, so taking it away would take them
// with it. What changed is where the picker is drawn, not how it is reached.

// branchRows is the picker's body: the filter and the count on one line, then
// as much of the list as the box has room for.
func (m Model) branchRows(room int) []string {
	if m.branchCreating {
		// The same box, asking the other question. The footer bar this
		// replaced put it at the bottom of the screen, away from the list it
		// was about.
		prompt := " " + m.styles.HelpKey.Render("new branch: ") + m.branchInput.View()
		if room <= 1 {
			return []string{prompt}
		}
		return []string{"", prompt}
	}

	list := m.activeBranches()

	// room is the whole budget, not the budget for the list. Returning more
	// than it means fitOverlay drops the overflow and prints a count in its
	// place — and the row it drops is the highlighted one, because that is
	// the one at the end.
	//
	// The filter is what you are typing and goes first whatever the room:
	// below three rows the blank line goes, and below two the list does.
	filter := m.branchFilterRow(len(list))
	if room <= 1 {
		return []string{filter}
	}

	if len(list) == 0 {
		return []string{filter, "", " " + m.styles.HelpDesc.Render("no matches")}[:min(room, 3)]
	}

	rows := []string{filter}
	if room > 2 {
		rows = append(rows, "")
	}

	// Scrolled like the agent picker and the file list: without an offset the
	// cursor walks off the bottom and enter then chooses a branch that is not
	// on screen.
	body := room - len(rows)
	first := scrollOffset(m.branchCursor, len(list), body)
	last := min(first+body, len(list))
	for i := first; i < last; i++ {
		rows = append(rows, m.branchRow(list[i], i == m.branchCursor, list[i] == m.currentBranch))
	}
	return rows
}

// branchFilterRow is what you are typing, with how much of the list it
// matches pushed to the right.
func (m Model) branchFilterRow(matched int) string {
	count := m.styles.HelpDesc.Render(fmt.Sprintf("%d/%d", matched, len(m.branches)))
	input := " " + m.branchFilter.View()
	gap := m.modalBodyWidth() - lipgloss.Width(input) - lipgloss.Width(count)
	if gap < 1 {
		return input
	}
	return input + strings.Repeat(" ", gap) + count
}

// branchRow is one branch, marked if it is the cursor's and if it is the one
// checked out.
func (m Model) branchRow(name string, selected, current bool) string {
	// The marker is a glyph, not a colour: #56's rule is that every
	// distinction carries a mark or a word as well as a hue.
	mark := "  "
	if current {
		mark = m.styles.StagedIcon.Render("* ")
	}
	// Before the name, not after it: a trailing marker on a list of ragged
	// names is easy to miss, and this is where the panel used to put it.
	room := max(m.modalBodyWidth()-5, 1)
	name = mark + truncatePath(name, room)
	if selected {
		return m.styles.Accent.Render(focusBar) + m.styles.PanelLabelFocus.Render(" "+name)
	}
	return "  " + name
}

// branchClosing is the box's last line: the keys it answers.
func (m Model) branchClosing() string {
	if m.branchCreating {
		return "enter creates · esc cancels"
	}
	if len(m.activeBranches()) == 0 {
		return "esc clears the filter"
	}
	return "type filters · ↑/↓ · enter switches · ^n new · esc closes"
}

// branchTitle says which of the two questions the box is asking.
func (m Model) branchTitle() string {
	if m.branchCreating {
		return " new branch"
	}
	return " branch"
}

// modalBodyWidth is the room a modal's body has, which the rows inside it
// have to fit. The border and the padding are not content.
func (m Model) modalBodyWidth() int {
	return max(m.modalBoxWidth()-2*modalPadding, 1)
}
