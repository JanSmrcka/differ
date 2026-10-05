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
	// headerSep joins the things the header names. One separator, so the bar
	// does not mix a double space with a middle dot on the same line.
	headerSep = " · "

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

	// Help, history and the problem are modals now (see Model.modal), drawn
	// over this like the pickers, so the view is still there around them.
	var body string
	switch {
	case m.onePanel():
		// One panel takes the terminal. A pair squeezed into sixty columns is
		// two unusable panels rather than one usable one, and the diff is what
		// differ is for — so the file list keeps the width only while it is
		// what the user is working in.
		only := m.rightPanel()
		if m.showsFileList() {
			only = m.leftPanel()
		}
		if m.showThemes {
			// One panel means the picker cannot sit beside the diff, so it
			// takes the panel outright. The preview still repaints the frame
			// around it, which is all the room there is.
			only = strings.Split(m.renderThemeOverlay(m.width, contentH), "\n")
		}
		body = strings.Join(padLines(only, contentH), "\n")
	default:
		left := padLines(m.leftPanel(), contentH)
		if m.showThemes {
			// The picker takes the file list's panel rather than the whole
			// area, so the diff beside it stays on screen — repainted in the
			// theme under the cursor, which is the point of a picker.
			left = padLines(strings.Split(m.renderThemeOverlay(m.listWidth(), contentH), "\n"), contentH)
		}
		right := padLines(m.rightPanel(), contentH)
		rows := make([]string, contentH)
		for i := range rows {
			rows[i] = m.panelRow(left[i], right[i])
		}
		body = strings.Join(rows, "\n")
	}

	// The modals go on top of whatever the switch produced, so the view is
	// still there around the box — you are commenting on a line, and choosing
	// where feedback goes while looking at what will be sent.
	if modal := m.modal(contentH); modal != "" {
		body = modalOver(strings.Split(body, "\n"), modal, m.width, contentH)
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
	// Always the file list. The branch picker used to be drawn here, which
	// meant choosing a branch cost you sight of the changeset; it is a box
	// over the view now, and the panel behind it keeps showing what you were
	// looking at.
	return append(m.panelHeader(m.leftPanelLabel(), m.changesetSummary(), m.listWidth(), m.focusOn(paneFiles)),
		strings.Split(m.renderFileList(), "\n")...)
}

// rightPanel is the diff, under a label naming the file on show.
func (m Model) rightPanel() []string {
	label, meta := m.diffLabel()
	return append(m.panelHeader(label, meta, m.diffWidth(), m.focusOn(paneDiff)),
		strings.Split(m.viewport.View(), "\n")...)
}

// panelHeader is a panel's one header row: the label on the left and that
// panel's own summary against the right edge — the same shape as every row
// beneath it, so a panel reads as a table rather than a caption over a list.
//
// It used to be two rows, the label and a blank one under it. The blank cost
// a row in both panels at every height and bought separation that the rule
// above the content already provides.
//
// The focused panel is marked with a bar, so focus survives a terminal
// without colour.
func (m Model) panelHeader(label, meta string, width int, focused bool) []string {
	mark, style := " ", m.styles.PanelLabel
	if focused {
		mark, style = m.styles.Accent.Render(focusBar), m.styles.PanelLabelFocus
	}
	// The meta ends in the column the rows' own right-hand column ends in,
	// which is what makes the header read as the top of a table rather than
	// a caption over one. One column goes to the focus mark.
	room := max(width-1, 0)
	gap := room - lipgloss.Width(label) - lipgloss.Width(meta)
	if meta == "" || gap < 1 {
		return []string{mark + style.Render(truncateEnd(label, room))}
	}
	return []string{mark + style.Render(label) + strings.Repeat(" ", gap) +
		m.styles.PanelLabel.Render(meta)}
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
	case modeDiff:
		return p == paneDiff
	default:
		return false
	}
}

func (m Model) leftPanelLabel() string { return "Files" }

// diffLabel names the file on show, and separately what is true of it. The
// two are returned apart because the panel header puts the state against the
// right edge, where the file rows keep their own.
func (m Model) diffLabel() (label, meta string) {
	if len(m.files) == 0 || m.cursor >= len(m.files) {
		return "", ""
	}
	f := m.files[m.cursor]
	var parts []string
	if f.change.Staged {
		parts = append(parts, "staged")
	}
	if m.session != nil {
		if n := m.session.CountFor(f.change.Path); n > 0 {
			parts = append(parts, plural(n, "comment"))
		}
	}
	return truncateEnd(f.change.Path, max(m.diffWidth()-2, 0)), strings.Join(parts, " · ")
}

// renderHeader is the one-line identity and summary bar.
func (m Model) renderHeader() string {
	name := m.styles.HeaderName.Render(" differ")
	ctx := m.styles.HeaderBranch.Render(m.headerContext())
	identity := name + m.styles.Chrome.Render(headerSep) + ctx

	summary := m.headerSummary()
	if summary == "" {
		return m.renderBar(lipgloss.NewStyle(), identity)
	}
	meta := m.styles.HeaderMeta.Render(summary + " ")

	gap := m.width - lipgloss.Width(identity) - lipgloss.Width(meta)
	if gap < 1 {
		// Too narrow for both: the identity and branch matter more.
		return m.renderBar(lipgloss.NewStyle(), identity)
	}
	return m.renderBar(lipgloss.NewStyle(), identity+strings.Repeat(" ", gap)+meta)
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
	return ctx
}

// headerSummary is what the header bar says on the right: how this branch
// stands against its upstream, and nothing else.
//
// It used to carry the file counts as well, which the file list's own header
// now states — and stating it twice on one screen is how a frame starts
// reading as chrome rather than as information.
func (m Model) headerSummary() string {
	if m.upstream.Upstream == "" || (m.upstream.Ahead == 0 && m.upstream.Behind == 0) {
		return ""
	}
	return fmt.Sprintf("↑%d ↓%d", m.upstream.Ahead, m.upstream.Behind)
}

// changesetSummary is the file list's own header: how many files, and how
// many of them are staged.
func (m Model) changesetSummary() string {
	if len(m.files) == 0 {
		return ""
	}
	staged := 0
	for _, f := range m.files {
		if f.change.Staged {
			staged++
		}
	}
	parts := []string{fmt.Sprintf("%d", len(m.files))}
	if staged > 0 {
		parts = append(parts, fmt.Sprintf("%d staged", staged))
	}
	return strings.Join(parts, " · ")
}

func (m Model) rule() string {
	return m.styles.Chrome.Render(strings.Repeat(horizontalRule, max(m.width, 0)))
}

// renderFooter is the bar below the content: hints, or an input when one is
// open.
func (m Model) renderFooter() string {
	// No budget arithmetic left here. Every input is a modal now; the comment
	// editor is the one exception, in a terminal too short for its box.
	var input string
	switch {
	case m.commenting && m.height < commentModalMinHeight:
		// Too short for a box. The footer form needs two rows and is what
		// this replaced, so it is still here for terminals the modal cannot
		// serve.
		input = m.renderCommentBar()
	default:
		return m.renderHintBar() // already carries the status row, and asks
		// for it itself — computing it above ran the whole thing twice on
		// every frame of the common path.
	}

	segment := m.statusSegment()

	// An open input replaces the hints, but not the status: "comment is empty
	// — esc to cancel" and "ai msg failed" are only reachable here, and the
	// whole point of those messages is not to fail silently.
	if segment != "" {
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
	// First, above even what just happened: it says the screen is not showing
	// the repository, and every other word in this row describes that screen.
	// Only where the key that clears it works. Outside the diff the bar was
	// still telling people to press R, which is unbound there.
	// A failure comes before everything, the notice included. An earlier
	// version put the notice first, reasoning that it describes the screen
	// every other word here describes — true, but not when the other word is a
	// failure the user has to act on. The notice is up to 56 columns, so at
	// eighty it clipped the failure's "! details" and below seventy-two it
	// pushed the failure off the row entirely.
	failed := m.problem != nil && m.statusMsg != ""
	if failed {
		parts = append(parts, m.statusMsg)
	}
	if m.mode == modeDiff && m.diffStale() {
		notice := "diff moved"
		// The summary is the first thing dropped when the row is tight: what
		// moved is available by reloading, and the half that says what to
		// press is not.
		if summary := m.changeSince(m.files); summary != "" && !failed && m.width >= noticeSummaryWidth {
			notice += " (" + summary + ")"
		}
		parts = append(parts, notice+" — "+reloadKey+" to reload")
	}
	if !failed && m.statusMsg != "" {
		parts = append(parts, m.statusMsg)
	}
	if m.mode == modeDiff {
		parts = append(parts, m.reviewSummary())
	}
	if m.splitDiff {
		parts = append(parts, "split")
	}

	// Cut here rather than leaving it to the bar, which drops words silently.
	return truncateEnd(strings.Join(parts, "  ·  "), max(m.width-1, 0))
}

// noticeSummaryWidth is the narrowest terminal that gets the "diff moved"
// notice with its summary. Below it the notice keeps only the part that says
// what to press.
const noticeSummaryWidth = 100

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
