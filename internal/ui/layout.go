package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The application frame.
//
// A compact header, a rule, the two panels separated by a vertical divider,
// a rule, then one bar carrying the key hints and whatever just happened.
// No boxes: the chrome is dim structure and the diff gets the room.

const (
	verticalDivider = "│"
	horizontalRule  = "─"
	focusBar        = "▍"
	panelGap        = 1 // one space either side of the divider

	verticalDividerWidth = 1
)

// chromeRows is what the frame spends on structure: the header and the two
// rules around the content.
const chromeRows = 3

func (m Model) View() string {
	if m.width == 0 || !m.ready {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("Terminal too small (%dx%d). Minimum: %dx%d", m.width, m.height, minWidth, minHeight)
	}

	contentH := m.contentHeight()

	// The help overlay takes the panel area rather than sitting under it, so
	// the layout's height does not change while it is open and the diff
	// viewport is exactly where it was when it closes.
	var body string
	switch {
	case m.showHelp:
		body = m.renderHelpOverlay(m.width, contentH)
	case m.showHistory:
		body = m.renderHistoryOverlay(m.width, contentH)
	case m.showProblem:
		body = m.renderProblemOverlay(m.width, contentH)
	case m.onePanel():
		// One panel takes the terminal. A pair squeezed into sixty columns is
		// two unusable panels rather than one usable one, and the diff is what
		// differ is for — so the file list keeps the width only while it is
		// what the user is working in.
		only := m.rightPanel()
		if m.showsFileList() {
			only = m.leftPanel()
		}
		body = strings.Join(padLines(only, contentH), "\n")
	default:
		left := padLines(m.leftPanel(), contentH)
		right := padLines(m.rightPanel(), contentH)
		rows := make([]string, contentH)
		for i := range rows {
			rows[i] = m.panelRow(left[i], right[i])
		}
		body = strings.Join(rows, "\n")
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		m.rule(),
		body,
		m.rule(),
		m.renderFooter(),
	)
}

// panelRow places one line from each panel either side of the divider.
func (m Model) panelRow(left, right string) string {
	gap := strings.Repeat(" ", panelGap)
	return padTo(left, m.listWidth()) + gap + m.styles.Chrome.Render(verticalDivider) + gap + right
}

// leftPanel is the file list, or the branch picker, under its own label.
func (m Model) leftPanel() []string {
	// The mode is chosen before rendering, not after: building the file list
	// and discarding it meant every keystroke in the branch filter paid for a
	// pass over every path in the changeset.
	body := m.renderBranchList(m.listHeight())
	if m.mode != modeBranchPicker {
		body = m.renderFileList()
	}
	return append(m.panelHeader(m.leftPanelLabel(), m.focusOn(paneFiles)), strings.Split(body, "\n")...)
}

// rightPanel is the diff, under a label naming the file on show.
func (m Model) rightPanel() []string {
	return append(m.panelHeader(m.diffLabel(), m.focusOn(paneDiff)), strings.Split(m.viewport.View(), "\n")...)
}

// panelHeader is a panel's label plus the blank line under it. The focused
// panel is marked with a bar, so focus survives a terminal without colour.
func (m Model) panelHeader(label string, focused bool) []string {
	if focused {
		return []string{m.styles.Accent.Render(focusBar) + m.styles.PanelLabelFocus.Render(label), ""}
	}
	return []string{" " + m.styles.PanelLabel.Render(label), ""}
}

// pane identifies a side of the layout.
type pane int

const (
	paneFiles pane = iota
	paneDiff
)

func (m Model) focusOn(p pane) bool {
	switch m.mode {
	case modeFileList, modeBranchPicker:
		return p == paneFiles
	case modeDiff, modeReview:
		return p == paneDiff
	default:
		return false
	}
}

func (m Model) leftPanelLabel() string {
	if m.mode == modeBranchPicker {
		return "BRANCHES"
	}
	return "CHANGED FILES"
}

// diffLabel names the file on show, with its staged and review state.
func (m Model) diffLabel() string {
	if len(m.files) == 0 || m.cursor >= len(m.files) {
		return ""
	}
	f := m.files[m.cursor]
	label := f.change.Path
	if f.change.Staged {
		label += "  staged"
	}
	if m.mode == modeReview && m.session != nil {
		if n := m.session.CountFor(f.change.Path); n > 0 {
			label += "  " + plural(n, "comment")
		}
	}
	return truncateEnd(label, max(m.diffWidth()-2, 0))
}

// renderHeader is the one-line identity and summary bar.
func (m Model) renderHeader() string {
	name := m.styles.HeaderName.Render(" differ")
	ctx := m.styles.HeaderBranch.Render(m.headerContext())
	summary := m.styles.HeaderMeta.Render(m.headerSummary() + " ")

	gap := m.width - lipgloss.Width(name) - lipgloss.Width(ctx) - lipgloss.Width(summary) - 2
	if gap < 1 {
		// Too narrow for both: the identity and branch matter more.
		return m.renderBar(lipgloss.NewStyle(), name+"  "+ctx)
	}
	return m.renderBar(lipgloss.NewStyle(), name+"  "+ctx+strings.Repeat(" ", gap)+summary)
}

// headerContext is the branch, what is being compared, and the active mode.
func (m Model) headerContext() string {
	ctx := m.branchName()
	switch {
	case m.ref != "":
		ctx += " ← " + m.ref
	case m.stagedOnly:
		ctx += " staged"
	}
	if m.mode == modeReview {
		ctx += "  ·  review"
	}
	return ctx
}

// headerSummary describes the changeset as a whole.
func (m Model) headerSummary() string {
	staged := 0
	for _, f := range m.files {
		if f.change.Staged {
			staged++
		}
	}
	parts := []string{plural(len(m.files), "file")}
	if staged > 0 {
		parts = append(parts, fmt.Sprintf("%d staged", staged))
	}
	if m.upstream.Upstream != "" && (m.upstream.Ahead > 0 || m.upstream.Behind > 0) {
		parts = append(parts, fmt.Sprintf("↑%d ↓%d", m.upstream.Ahead, m.upstream.Behind))
	}
	return strings.Join(parts, " · ")
}

func (m Model) rule() string {
	return m.styles.Chrome.Render(strings.Repeat(horizontalRule, max(m.width, 0)))
}

// renderFooter is the bar below the content: hints, or an input when one is
// open.
func (m Model) renderFooter() string {
	var input string
	switch {
	case m.commenting:
		input = m.renderCommentEditor()
	case m.mode == modeCommit:
		input = m.renderCommitBar()
	case m.mode == modeBranchPicker && m.branchCreating:
		input = m.renderBranchCreateBar()
	default:
		return m.renderHintBar() // already carries the status row
	}

	// An open input replaces the hints, but not the status: "comment is empty
	// — esc to cancel" and "ai msg failed" are only reachable here, and the
	// whole point of those messages is not to fail silently.
	if segment := m.statusSegment(); segment != "" {
		return lipgloss.JoinVertical(lipgloss.Left,
			input,
			m.renderBar(m.styles.StatusText, " "+segment),
		)
	}
	return input
}

// renderHintBar is the command bar, with a status row beneath it when there
// is something to say.
//
// The two are deliberately never packed onto one line. Doing so made the
// footer's height depend on the *length* of the status text, so the panels
// resized as messages came and went — and the diff viewport would not return
// to its previous height after an overlay closed.
func (m Model) renderHintBar() string {
	hints := m.renderCommandBar()

	segment := m.statusSegment()
	if segment == "" {
		return hints
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		hints,
		m.renderBar(m.styles.StatusText, " "+segment),
	)
}

// statusSegment is the transient right-hand side of the footer: mode-specific
// state and what just happened. The changeset counts live in the header, so
// they are not repeated here.
func (m Model) statusSegment() string {
	// What just happened comes first, and the standing state after it. The
	// row is one line and the bar drops whole words off the end to keep it
	// that way, so the order here is a priority order: with the review
	// progress and "split" in front, a failure at sixty columns was cut down
	// to its first few words — "generating a commit message", with no
	// "failed", no hint and no "!" — and read as progress rather than a
	// failure.
	var parts []string
	if m.statusMsg != "" {
		parts = append(parts, m.statusMsg)
	}
	if m.mode == modeReview {
		parts = append(parts, m.reviewSummary())
	}
	if m.splitDiff {
		parts = append(parts, "split")
	}

	// Cut here rather than leaving it to the bar, which drops words silently.
	return truncateEnd(strings.Join(parts, "  ·  "), max(m.width-1, 0))
}

func padLines(lines []string, height int) []string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	if height < 0 {
		return nil
	}
	return lines[:height]
}

func padTo(s string, w int) string {
	if pad := w - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}
