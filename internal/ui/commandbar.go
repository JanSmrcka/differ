package ui

import (
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
	default:
		return true
	}
}

// renderCommandBar is the one-line footer of available commands.
//
// It is always exactly one row. Where it does not fit, items are dropped from
// the middle rather than wrapped, and the two that get the user out — help and
// quit — are kept whatever the width.
func (m Model) renderCommandBar() string {
	if m.width <= 0 {
		return ""
	}
	items := m.barBindings()

	// Help and quit are pinned to the end and never dropped.
	var head, tail []string
	for _, b := range items {
		if b.Desc == "quit" {
			continue
		}
		head = append(head, m.renderBarItem(b))
	}
	for _, b := range globalBindings() {
		if b.Bar {
			tail = append(tail, m.renderBarItem(b))
		}
	}
	tail = append(tail, m.renderBarItem(binding{Keys: []string{"q"}, Desc: "quit"}))

	return m.renderBar(lipgloss.NewStyle(), " "+fitBarItems(head, tail, m.width-1))
}

func (m Model) renderBarItem(b binding) string {
	return m.styles.HelpKey.Render(b.label()) + " " + m.styles.HelpDesc.Render(b.Desc)
}

// fitBarItems joins as many head items as fit, always keeping the tail.
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

// renderHelpOverlay lists every command in the current mode.
//
// It is drawn over the panel area rather than added below it, so opening it
// does not change the layout's height — the diff viewport must not resize and
// fail to come back.
func (m Model) renderHelpOverlay(width, height int) string {
	rows := make([]string, 0, height)
	title := m.styles.HelpKey.Render(" keys · " + modeName(m.mode))
	rows = append(rows, title, "")

	for _, b := range append(keymapFor(m.mode), globalBindings()...) {
		if b.Desc == "" && b.Help == "" {
			continue // an alias row, already covered by its label
		}
		key := m.styles.HelpKey.Render(padTo(" "+b.label(), 14))
		text := b.help()
		if b.Confirm {
			text += m.styles.HelpDesc.Render("  (asks again)")
		}
		rows = append(rows, key+m.styles.HelpDesc.Render(text))
	}
	rows = append(rows, "", m.styles.HelpDesc.Render(" ? or esc to close"))

	for i, r := range rows {
		rows[i] = padTo(r, width)
	}
	for len(rows) < height {
		rows = append(rows, padTo("", width))
	}
	return strings.Join(rows[:height], "\n")
}

// modeName is what the mode is called in the help overlay's title.
func modeName(mode viewMode) string {
	switch mode {
	case modeDiff:
		return "diff"
	case modeReview:
		return "review"
	case modeCommit:
		return "commit"
	case modeBranchPicker:
		return "branches"
	default:
		return "files"
	}
}
