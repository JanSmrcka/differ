package ui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

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
	m.agentFilter = ""
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
	mux, muxErr, repo := m.mux, m.muxErr, m.repo
	return func() tea.Msg {
		if mux == nil {
			return agentsLoadedMsg{err: muxErr}
		}
		ctx, cancel := context.WithTimeout(context.Background(), agentScanTimeout)
		defer cancel()
		// The repository is passed rather than the ordering done here:
		// forgetting the one call left the picker unsorted with every test
		// green. Asked here, off the update loop, because it is a git call.
		var info feedback.RepoInfo
		if repo != nil {
			info.Root = repo.Dir()
			info.CommonDir, _ = repo.CommonDir()
		}
		found, err := mux.Agents(ctx, info)
		return agentsLoadedMsg{agents: found, err: err}
	}
}

// feedbackConfigOf is the target configuration the config file describes.
func feedbackConfigOf(cfg config.Config, env feedback.Env) feedback.Config {
	return feedback.Config{
		Target:       cfg.FeedbackTarget,
		TmuxTarget:   cfg.TmuxTarget,
		HerdrTarget:  cfg.HerdrTarget,
		HerdrPane:    cfg.HerdrPane,
		ZellijTarget: cfg.ZellijTarget,
		Env:          env,
	}
}

// applyChoice writes a chosen agent's target into the config. Each field is
// written only when the choice has one, so choosing a herdr agent leaves
// tmux_target as it was — the picker never asks which multiplexer it was.
func applyChoice(cfg *config.Config, fc feedback.Config) {
	cfg.FeedbackTarget = fc.Target
	if fc.TmuxTarget != "" {
		cfg.TmuxTarget = fc.TmuxTarget
	}
	if fc.ZellijTarget != "" {
		cfg.ZellijTarget = fc.ZellijTarget
	}
	if fc.HerdrPane != "" || fc.HerdrTarget != "" {
		cfg.HerdrTarget, cfg.HerdrPane = fc.HerdrTarget, fc.HerdrPane
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
	// Within what the filter leaves: it can be typed while the scan is out,
	// and the cursor indexes visibleAgents, not m.agents.
	inUse := feedbackConfigOf(m.cfg, m.feedbackEnv)
	for i, a := range m.visibleAgents() {
		if a.Matches(inUse) {
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
	picked := m.chosenAgent()
	if picked == nil {
		// Nothing under the cursor — a filter with no matches. Closing on it
		// would throw the filter away for nothing.
		if len(m.agents) == 0 {
			m.showAgents = false
		}
		return m, nil
	}
	m.showAgents = false
	chosen := *picked
	applyChoice(&m.cfg, chosen.FeedbackConfig())
	// Re-resolve, or the choice changes nothing until a restart. m.target is
	// the object send() uses and it was built once in NewModel, so writing the
	// config alone left the bar saying "sending to claude" while the review
	// went to the clipboard.
	m.target, m.targetErr = feedback.Resolve(feedbackConfigOf(m.cfg, m.feedbackEnv))
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
	m.agentCursor = clampCursor(m.agentCursor+delta, len(m.visibleAgents()))
	return m, nil
}

// visibleAgents is the list the filter leaves, in the scan's order. The
// cursor indexes this, not m.agents.
func (m Model) visibleAgents() []feedback.Agent {
	if m.agentFilter == "" {
		return m.agents
	}
	needle := strings.ToLower(m.agentFilter)
	var out []feedback.Agent
	for _, a := range m.agents {
		hay := strings.ToLower(strings.Join([]string{
			a.Label(), a.Workspace, a.Tool, a.State, a.Title, a.Dir,
		}, "\x00"))
		if strings.Contains(hay, needle) {
			out = append(out, a)
		}
	}
	return out
}

// chosenAgent is the agent under the cursor, or nil when the filter leaves
// none.
func (m Model) chosenAgent() *feedback.Agent {
	visible := m.visibleAgents()
	if m.agentCursor < 0 || m.agentCursor >= len(visible) {
		return nil
	}
	return &visible[m.agentCursor]
}

// setAgentFilter changes the filter and keeps the cursor on the same agent
// when it is still listed. Narrowing a list must not quietly swap the choice
// for whichever agent now happens to sit at the same index.
func (m Model) setAgentFilter(filter string) Model {
	pane := ""
	if a := m.chosenAgent(); a != nil {
		pane = a.Pane
	}
	m.agentFilter = filter
	m.agentCursor = 0
	for i, a := range m.visibleAgents() {
		if a.Pane == pane {
			m.agentCursor = i
			break
		}
	}
	return m
}

// agentClosing is what the picker says it will answer to, which depends on
// whether there is anything to choose.
func (m Model) agentClosing() string {
	switch {
	case !m.agentsScanned:
		return "esc cancels"
	case len(m.agents) == 0:
		return "esc closes"
	case len(m.visibleAgents()) == 0:
		return "esc clears the filter"
	}
	return "type filters · ↑/↓ · enter chooses · esc closes"
}

// agentRows is the picker's content.
func (m Model) agentRows(room int) []string {
	if !m.agentsScanned {
		return []string{"", " looking for agents…"}
	}
	if len(m.agents) == 0 {
		return m.noAgentRows()
	}

	// The filter goes first whatever the room, as in the branch picker:
	// below three rows the blank line goes, and below two the list does.
	visible := m.visibleAgents()
	rows := []string{m.agentFilterRow(len(visible))}
	if room <= 1 {
		return rows
	}
	if room > 2 {
		rows = append(rows, "")
	}
	if len(visible) == 0 {
		return append(rows, " "+m.styles.HelpDesc.Render("no matches"))[:min(room, 3)]
	}

	// Scrolled, like the file list. Without an offset fitOverlay dropped the
	// overflow and printed how many rows it had dropped, so pressing j past
	// the edge left nothing highlighted anywhere and enter then chose an
	// agent that was not on screen.
	inUse := feedbackConfigOf(m.cfg, m.feedbackEnv)
	body := room - len(rows)
	first := scrollOffset(m.agentCursor, len(visible), body)
	last := min(first+body, len(visible))
	for i := first; i < last; i++ {
		rows = append(rows, m.agentRow(visible[i], i == m.agentCursor, visible[i].Matches(inUse)))
	}
	return rows
}

// noAgentRows is the empty state.
//
// Saying what was looked for is the difference between "nothing here" and
// "differ is broken". The multiplexer says it, so the picker does not need to
// know which one it asked.
func (m Model) noAgentRows() []string {
	name, searched := "tmux, herdr or zellij", []string(nil)
	if m.mux != nil {
		name, searched = m.mux.Name(), m.mux.Searched()
	}
	rows := []string{"", " No agent is running in " + name + ".", ""}
	for _, line := range searched {
		rows = append(rows, " "+line)
	}
	return append(rows, "", " Start one and press "+agentKey+" again.")
}

// agentFilterRow is what you are typing, with how much of the list it
// matches pushed to the right — the branch picker's filter row.
func (m Model) agentFilterRow(matched int) string {
	input := " " + m.styles.HelpDesc.Render("filter…")
	if m.agentFilter != "" {
		input = " " + m.agentFilter
	}
	count := m.styles.HelpDesc.Render(fmt.Sprintf("%d/%d", matched, len(m.agents)))
	gap := m.modalBodyWidth() - lipgloss.Width(input) - lipgloss.Width(count)
	if gap < 1 {
		return input
	}
	return input + strings.Repeat(" ", gap) + count
}

// agentRow is one agent: label, whether it is in use, tool, state, and what
// tells it apart.
func (m Model) agentRow(a feedback.Agent, selected, inUse bool) string {
	width := m.modalWidth() - 2*modalPadding - 2
	// The pane id disambiguates two agents in one window, which share a
	// session:window label and usually a directory too.
	name := a.Label()
	if m.labelIsAmbiguous(a) {
		name += " " + a.Pane
	}
	label := "  " + name
	if selected {
		label = m.styles.Selected.Render(cursorMarker + " " + name)
	}
	if inUse {
		label += m.styles.HelpDesc.Render("  ·  in use")
	}
	row := label + "  " + m.styles.CommentMeta.Render(a.Tool)
	// The state is a word, not a colour: waiting and working have to be told
	// apart with the colour stripped.
	if a.State != "" {
		row += "  " + m.styles.CommentMeta.Render(a.State)
	}
	// What tells two agents apart gets whatever room is left rather than
	// being dropped first: the agent's own title when it reports one, else
	// the directory it is in.
	if room := width - lipgloss.Width(row) - 3; room > 12 {
		if a.Title != "" {
			row += "  " + m.styles.HelpDesc.Render(truncateEnd(a.Title, room))
		} else {
			row += "  " + m.styles.HelpDesc.Render(truncatePath(a.Dir, room))
		}
	}
	return row
}

// agentPickerKey handles the picker's keys while it is open.
//
// No "handled" return: every key is handled, because the picker owns the
// keyboard while it is open. A stray key reaching the repository behind an
// overlay would be acting on something the user cannot see.
//
// The branch picker's idiom: any printable key types into the filter, so
// moving is the arrows or ^j/^k, and esc clears the filter before it closes.
func (m Model) agentPickerKey(key string) (Model, tea.Cmd) {
	switch key {
	case "down", "ctrl+j":
		return m.moveAgentCursor(1)
	case "up", "ctrl+k":
		return m.moveAgentCursor(-1)
	case "enter":
		return m.confirmAgent()
	case "backspace":
		if r := []rune(m.agentFilter); len(r) > 0 {
			return m.setAgentFilter(string(r[:len(r)-1])), nil
		}
		return m, nil
	case "esc":
		if m.agentFilter != "" {
			return m.setAgentFilter(""), nil
		}
		return m.cancelAgentPicker()
	}
	if r := []rune(key); len(r) == 1 && unicode.IsPrint(r[0]) {
		return m.setAgentFilter(m.agentFilter + key), nil
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
