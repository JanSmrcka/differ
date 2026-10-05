package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The command bar and the help overlay, both built from the keymap.
//
// The bar replaced a fixed dot-separated list per mode. That list was written
// by hand next to the handlers, was long enough to wrap on a narrow terminal —
// which silently made the footer two rows tall and pushed the top of the
// layout off screen — and showed keys that had nothing to do with what the
// user was doing.

// typing reports whether keys are going into an input rather than acting as
// commands. ? is a perfectly ordinary character in a commit message.
func (m Model) typing() bool {
	return m.commenting || m.mode == modeCommit ||
		m.mode == modeBranchPicker || m.branchCreating
}

// barSeparator is deliberately plain: the bar is read at a glance, and heavy
// punctuation between items reads as noise.
const barSeparator = "   "

// barBindings are the commands worth offering right now.
//
// The keymap says which commands belong in the bar; this decides which of
// those would actually do something, so nothing is advertised that cannot be
// used yet.
func (m Model) barBindings() []binding {
	var out []binding
	for _, b := range keymapFor(m.mode) {
		if !b.Bar || b.Desc == "" {
			continue
		}
		if !m.bindingApplies(b) {
			continue
		}
		out = append(out, b)
	}
	return out
}

// bindingApplies filters the state-dependent commands.
func (m Model) bindingApplies(b binding) bool {
	switch b.Desc {
	case "send":
		// Offering send with nothing to send is the sort of dead control the
		// bar exists to avoid.
		return m.reviewProgress().Pending > 0
	case "stage", "stage all":
		// Staging is meaningless when looking at the index or a ref.
		return !m.stagedOnly && m.ref == ""
	case "comment", "hunk comment", "delete":
		// Nothing to comment on, so nothing to offer. The diff can be open
		// with an empty changeset — that is how `differ review` says there is
		// nothing to review — and every one of these would return early.
		return len(m.files) > 0
	default:
		return true
	}
}

// renderCommandBar is the one-line footer of available commands.
//
// It is always exactly one row. Where it does not fit, items are dropped from
// the end of the list rather than wrapped, so the most important survive; the
// pinned tail — help and quit — is kept whatever the width.
func (m Model) renderCommandBar() string {
	if m.width <= 0 {
		return ""
	}
	// A modal owns the keyboard, so the bar must not advertise keys that
	// would only type a character into it. With the comment editor open it
	// was still offering "c comment  C hunk comment  r exit review", every
	// one of which inserts a letter; with the picker open it offered the
	// file-list keys, all of which it swallows.
	if m.showAgents || m.showThemes || m.commenting || m.showHelp || m.showHistory || m.showProblem {
		return m.renderBar(m.styles.HelpDesc, " "+m.modalKeys())
	}
	items := m.barBindings()

	// Help and quit are pinned to the end and never dropped — but only where
	// they are keys at all. In the branch picker and the commit input every
	// character goes into the field, so advertising ? and q there would be
	// advertising two ways to type a letter.
	var head, tail []string
	for _, b := range items {
		switch {
		case b.Desc == "quit":
			// Pinned below rather than dropped with the rest.
		case m.typing() && isExit(b):
			// This mode's way out is what gets pinned instead.
		default:
			head = append(head, m.renderBarItem(b))
		}
	}
	if m.typing() {
		// ? and q are characters here, so the pinned item is whatever closes
		// the input. Something has to be pinned: a bar with an empty tail
		// renders nothing at all once the terminal is narrow enough.
		for _, b := range items {
			if isExit(b) {
				tail = append(tail, m.renderBarItem(b))
				break
			}
		}
	} else {
		for _, b := range globalBindings() {
			if b.Bar {
				tail = append(tail, m.renderBarItem(b))
			}
		}
		tail = append(tail, m.renderBarItem(binding{Keys: []string{"q"}, Desc: "quit"}))
	}

	return m.renderBar(lipgloss.NewStyle(), " "+fitBarItems(head, tail, m.width-1))
}

// isExit spots the binding that leaves the current view, which is the one the
// bar must never drop.
func isExit(b binding) bool {
	return len(b.Keys) > 0 && b.Keys[0] == "esc"
}

func (m Model) renderBarItem(b binding) string {
	return m.styles.HelpKey.Render(b.label()) + " " + m.styles.HelpDesc.Render(b.Desc)
}

// fitBarItems joins as many leading head items as fit, always keeping the
// tail. The keymap's order is therefore its priority order.
func fitBarItems(head, tail []string, width int) string {
	fixed := strings.Join(tail, barSeparator)
	for n := len(head); n > 0; n-- {
		line := strings.Join(append(head[:n:n], fixed), barSeparator)
		if lipgloss.Width(line) <= width {
			return line
		}
	}
	return fixed
}

// renderHelpOverlay lists every command in the current mode, fitted into
// width by height. On screen it is a modal (see Model.modal).
func (m Model) renderHelpOverlay(width, height int) string {
	return m.fitOverlay(m.helpTitle(), m.helpRows(), helpClosing, width, height)
}

const helpClosing = "? or esc to close"

func (m Model) helpTitle() string { return " keys · " + modeName(m.mode) }

// helpRows is one row per command this view offers.
func (m Model) helpRows() []string {
	var rows []string
	for _, b := range append(keymapFor(m.mode), globalBindings()...) {
		if b.Desc == "" && b.Help == "" {
			continue // an alias row, already covered by its label
		}
		// The same filter the bar uses. Listing a command here that would
		// return early is the dead control the bar exists to avoid, just
		// moved somewhere less visible.
		if !m.bindingApplies(b) {
			continue
		}
		key := m.styles.HelpKey.Render(padTo(" "+b.label(), 14))
		text := b.help()
		if b.Confirm {
			text += m.styles.HelpDesc.Render("  (asks again)")
		}
		rows = append(rows, key+m.styles.HelpDesc.Render(text))
	}
	return rows
}

// fitOverlay lays a title, a body and a closing line into exactly the panel
// area, and is the only place either overlay decides what to drop.
//
// Both dimensions matter. A row wider than the terminal soft-wraps, the body
// gains a line, and the bottom rule and command bar are pushed off screen —
// the very failure overlays are drawn over the panels to avoid. A body taller
// than the panel used to be cut at the end, which silently took the closing
// line with it: at the minimum 80x10 terminal the help overlay showed three of
// twenty-one keys and no way out. The closing line is now kept whatever
// happens, and what was dropped is counted.
//
// Rows arrive already styled, so the width cut cannot be a rune slice: an
// escape sequence measures zero columns, and dropping runes off the end takes
// the reset with it — the colour then bleeds down the rest of the screen — or
// cuts an escape in half. MaxWidth understands escapes and closes what it
// cuts; the ellipsis goes on afterwards, outside the styled text.
func (m Model) fitOverlay(title string, body []string, closing string, width, height int) string {
	footer := m.styles.HelpDesc.Render(" " + closing)

	// The title, the blank line under it, the blank line above the footer and
	// the footer itself.
	const chrome = 4
	switch room := height - chrome; {
	case room <= 0:
		body = nil
	case len(body) > room:
		// One of the rows left goes to saying how many are not shown.
		keep := room - 1
		hidden := len(body) - keep
		body = append(body[:keep:keep],
			m.styles.HelpDesc.Render(fmt.Sprintf(" … %d more", hidden)))
	}

	rows := append([]string{m.styles.HelpKey.Render(title), ""}, body...)
	rows = append(rows, "", footer)

	for i, r := range rows {
		rows[i] = padTo(clipOverlayRow(r, width), width)
	}
	for len(rows) < height {
		rows = append(rows, padTo("", width))
	}
	return strings.Join(rows[:max(height, 0)], "\n")
}

// clipOverlayRow cuts one row to the width, marking the cut.
func clipOverlayRow(row string, width int) string {
	if width <= 0 || lipgloss.Width(row) <= width {
		return row
	}
	if width == 1 {
		// MaxWidth(0) does not truncate at all, so the row would pass through
		// whole and wrap.
		return "…"
	}
	return lipgloss.NewStyle().MaxWidth(width-1).Render(row) + "…"
}

// modeName is what the mode is called in the help overlay's title.
func modeName(mode viewMode) string {
	switch mode {
	case modeDiff:
		return "diff"
	case modeCommit:
		return "commit"
	case modeBranchPicker:
		return "branches"
	default:
		return "files"
	}
}

// modalKeys is what the bar says while a modal is open: the keys that modal
// answers, and nothing else.
func (m Model) modalKeys() string {
	switch {
	case m.commenting:
		return commentClosing
	case m.showAgents:
		return m.agentClosing()
	case m.showThemes:
		return "j/k · enter keeps · esc cancels"
	case m.showHelp:
		return helpClosing
	case m.showHistory:
		return historyClosing
	case m.showProblem:
		return problemClosing
	}
	return ""
}
