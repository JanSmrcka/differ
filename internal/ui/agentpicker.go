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
		found, err := feedback.Agents(ctx)
		// Asked here rather than kept on the model: differ can be moved
		// between tmux sessions while it runs.
		feedback.SortAgents(found, feedback.OwnSession(ctx), root)
		return agentsLoadedMsg{agents: found, err: err}
	}
}

// handleAgentsLoaded installs the list, with the cursor on the agent already
// chosen if it is still there.
func (m Model) handleAgentsLoaded(msg agentsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.showAgents = false
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
	m.showAgents = false
	if m.agentCursor >= len(m.agents) {
		return m, nil
	}
	chosen := m.agents[m.agentCursor]
	m.cfg.TmuxTarget = chosen.Pane
	m.cfg.FeedbackTarget = "tmux"
	m.statusMsg = "sending to " + chosen.Tool + " in " + chosen.Label()

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
func (m Model) agentRows() []string {
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

	rows := make([]string, 0, len(m.agents))
	for i, a := range m.agents {
		label := "  " + a.Label()
		if i == m.agentCursor {
			label = m.styles.Accent.Render(focusBar) + m.styles.PanelLabelFocus.Render(" "+a.Label())
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
