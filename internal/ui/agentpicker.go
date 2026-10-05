package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/config"
	"github.com/jansmrcka/differ/internal/feedback"
)

// Choosing which agent the review goes to.
//
// tmux_target had to be written into the config by hand, and unset it meant
// the last active pane — right in a two-pane layout and wrong in every other.
// With several agents running there was no way to say which, and no way to see
// which differ would pick.

// agentKey opens the picker. A for agent; the review keys are taken.
const agentKey = "A"

// agentScanTimeout bounds the discovery. It is two subprocesses, but tmux on a
// wedged server can block, and the picker must not.
const agentScanTimeout = 3 * time.Second

// openAgentPicker shows the picker and starts looking. Discovery runs two
// subprocesses, so it happens off the update loop.
func (m Model) openAgentPicker() (Model, tea.Cmd) {
	m.showAgents = true
	m.agentsAfterSendFailure = false
	m.agents, m.agentsScanned = nil, false
	m.showHelp, m.showHistory, m.showProblem = false, false, false
	if m.showThemes {
		// The theme picker changes the session as you move through it, so it
		// is closed properly rather than dropped.
		var restore tea.Cmd
		m, restore = m.cancelTheme()
		return m, tea.Batch(restore, m.scanAgentsCmd())
	}
	return m, m.scanAgentsCmd()
}

func (m Model) scanAgentsCmd() tea.Cmd {
	root := ""
	if m.repo != nil {
		root = m.repo.Dir()
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), agentScanTimeout)
		defer cancel()
		// The root is passed rather than the ordering done here: forgetting
		// the one call left the picker unsorted with every test green.
		found, err := feedback.Agents(ctx, root)
		return agentsLoadedMsg{agents: found, err: err}
	}
}

// handleAgentsLoaded installs the list, with the cursor on the agent already
// chosen if it is still there.
func (m Model) handleAgentsLoaded(msg agentsLoadedMsg) (tea.Model, tea.Cmd) {
	if !m.showAgents {
		// The scan takes up to three seconds and the user can leave before it
		// answers. A failure they are no longer waiting for is not worth the
		// status bar, and a list is not worth installing into a closed
		// picker.
		return m, nil
	}
	if msg.err != nil {
		m.showAgents = false
		if m.agentsAfterSendFailure {
			// The picker only opened because a send failed. Reporting the
			// scan through fail would replace that problem, and `!` would
			// then no longer say why the review did not arrive.
			//
			// The bar is left alone as well: overwriting it took away both
			// the line saying the review did not arrive and the `!` that
			// offers the rest, which is the failure problem.line exists to
			// prevent. The scan's own failure is the lesser event and the
			// closed picker already shows it produced nothing.
			return m, nil
		}
		return m.fail("looking for agents", msg.err), nil
	}
	m.agents, m.agentsScanned = msg.agents, true
	m.agentCursor = 0
	for i, a := range m.agents {
		if a.Pane == m.cfg.TmuxTarget {
			m.agentCursor = i
			break
		}
	}
	return m, nil
}

// confirmAgent remembers the choice, and sets the target that uses it.
//
// Choosing a pane and leaving feedback_target on clipboard would be a picker
// that does nothing, so the two are set together.
func (m Model) confirmAgent() (Model, tea.Cmd) {
	// Nothing to confirm yet. Closing the picker first threw the scan away:
	// the list arrived a moment later, found the picker shut and was
	// discarded, so enter during "looking for agents…" left no picker, no
	// list and nothing said.
	if !m.agentsScanned {
		return m, nil
	}
	m.showAgents = false
	if m.agentCursor < 0 || m.agentCursor >= len(m.agents) {
		return m, nil
	}
	chosen := m.agents[m.agentCursor]
	m.cfg.TmuxTarget = chosen.Pane
	m.cfg.FeedbackTarget = "tmux"
	// Re-resolve, or the choice changes nothing until a restart. m.target is
	// the object send() uses and it was built once in NewModel, so writing the
	// config alone left the bar saying "sending to claude" while the review
	// went to the clipboard.
	m.target, m.targetErr = feedback.Resolve(feedback.Config{
		Target:     m.cfg.FeedbackTarget,
		TmuxTarget: m.cfg.TmuxTarget,
	})
	m.statusMsg = "sending to " + chosen.Tool + " in " + chosen.Label()
	if m.targetErr != nil {
		return m.fail("choosing an agent", m.targetErr), nil
	}

	cfg := m.cfg
	return m, func() tea.Msg { return savePrefDoneMsg{err: config.Save(cfg)} }
}

func (m Model) cancelAgentPicker() (Model, tea.Cmd) {
	m.showAgents = false
	return m, nil
}

func (m Model) moveAgentCursor(delta int) (Model, tea.Cmd) {
	m.agentCursor = clampCursor(m.agentCursor+delta, len(m.agents))
	return m, nil
}

// agentClosing is what the picker says it will answer to, which depends on
// whether there is anything to choose.
func (m Model) agentClosing() string {
	switch {
	case !m.agentsScanned:
		return "esc cancels"
	case len(m.agents) == 0:
		return "esc closes"
	}
	return "j/k · enter chooses · esc cancels"
}

// agentRows is the picker's content.
func (m Model) agentRows(room int) []string {
	width := m.modalWidth() - 2*modalPadding - 2
	if !m.agentsScanned {
		return []string{"", " looking for agents…"}
	}
	if len(m.agents) == 0 {
		// Saying what was looked for is the difference between "nothing here"
		// and "differ is broken".
		return []string{
			"",
			" No agent is running in tmux.",
			"",
			" differ looks for claude, codex, gemini,",
			" copilot, opencode and aider in every pane",
			" on this tmux server.",
			"",
			" Start one and press " + agentKey + " again.",
		}
	}

	// Scrolled, like the file list. Without an offset fitOverlay dropped the
	// overflow and printed how many rows it had dropped, so pressing j past
	// the edge left nothing highlighted anywhere and enter then chose an
	// agent that was not on screen.
	first := scrollOffset(m.agentCursor, len(m.agents), room)
	last := min(first+room, len(m.agents))

	rows := make([]string, 0, last-first)
	for i := first; i < last; i++ {
		a := m.agents[i]
		// The pane id disambiguates two agents in one window, which share a
		// session:window label and usually a directory too.
		name := a.Label()
		if m.labelIsAmbiguous(a) {
			name += " " + a.Pane
		}
		label := "  " + name
		if i == m.agentCursor {
			label = m.styles.Selected.Render(cursorMarker + " " + name)
		}
		if a.Pane == m.cfg.TmuxTarget {
			label += m.styles.HelpDesc.Render("  ·  in use")
		}
		row := label + "  " + m.styles.CommentMeta.Render(a.Tool)
		// The directory is what tells two agents in one session apart, so it
		// gets whatever room is left rather than being dropped first.
		if room := width - lipgloss.Width(row) - 3; room > 12 {
			row += "  " + m.styles.HelpDesc.Render(truncatePath(a.Dir, room))
		}
		rows = append(rows, row)
	}
	return rows
}

// agentPickerKey handles the picker's keys while it is open.
//
// No "handled" return: every key is handled, because the picker owns the
// keyboard while it is open. A stray key reaching the repository behind an
// overlay would be acting on something the user cannot see.
func (m Model) agentPickerKey(key string) (Model, tea.Cmd) {
	switch key {
	case "j", "down":
		return m.moveAgentCursor(1)
	case "k", "up":
		return m.moveAgentCursor(-1)
	case "enter":
		return m.confirmAgent()
	case "esc", "q", agentKey:
		return m.cancelAgentPicker()
	}
	return m, nil
}

// scrollOffset is the first row to draw so that cursor is among the room rows
// on screen.
//
// The same arithmetic as the file list and the log browser; a third copy is
// not worth a package, but a third divergence would be.
func scrollOffset(cursor, total, room int) int {
	if room <= 0 || total <= room {
		return 0
	}
	first := cursor - room/2
	if first < 0 {
		first = 0
	}
	if first > total-room {
		first = total - room
	}
	return first
}

// labelIsAmbiguous reports whether another agent shows the same label.
func (m Model) labelIsAmbiguous(a feedback.Agent) bool {
	seen := 0
	for _, other := range m.agents {
		if other.Label() == a.Label() {
			seen++
		}
	}
	return seen > 1
}
