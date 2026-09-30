package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/feedback"
	"github.com/jansmrcka/differ/internal/testutil"
)

func withAgents(t *testing.T, m Model, agents ...feedback.Agent) Model {
	t.Helper()
	updated, _ := m.Update(agentsLoadedMsg{agents: agents})
	return updated.(Model)
}

func twoAgents() []feedback.Agent {
	return []feedback.Agent{
		{Pane: "%10", Session: "differ", Window: "2", Tool: "claude", Dir: "/repo"},
		{Pane: "%5", Session: "web", Window: "1", Tool: "opencode", Dir: "/elsewhere"},
	}
}

// A opens the picker and starts looking. Discovery runs two subprocesses, so
// it must not happen in Update.
func TestAgentPicker_OpensAndScansOffTheUpdateLoop(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})

	updated, cmd := m.Update(key(agentKey))
	m = updated.(Model)
	if !m.showAgents {
		t.Fatal("A did not open the picker")
	}
	if cmd == nil {
		t.Fatal("A did not start a scan")
	}
	// Until the scan answers, the picker says it is looking rather than
	// claiming there is nothing.
	if got := m.View(); !strings.Contains(got, "looking for agents") {
		t.Errorf("the picker does not say it is still looking:\n%s", got)
	}
}

// The list, with the one already chosen marked.
func TestAgentPicker_ShowsEachAgentAndWhichIsInUse(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m.cfg.TmuxTarget = "%5"
	m, _ = m.openAgentPicker()
	m = withAgents(t, m, twoAgents()...)

	view := m.View()
	for _, want := range []string{"differ:2", "claude", "web:1", "opencode", "in use"} {
		if !strings.Contains(view, want) {
			t.Errorf("the picker does not show %q:\n%s", want, view)
		}
	}
	// The cursor starts on the one in use, not at the top.
	if m.agents[m.agentCursor].Pane != "%5" {
		t.Errorf("the cursor starts on %q, not the agent in use",
			m.agents[m.agentCursor].Pane)
	}
}

// Choosing writes both the pane and the target that uses it. Writing only the
// pane would be a picker that does nothing.
func TestAgentPicker_ChoosingSetsThePaneAndTheTarget(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m.cfg.FeedbackTarget = "clipboard"
	m, _ = m.openAgentPicker()
	m = withAgents(t, m, twoAgents()...)

	m, cmd := m.confirmAgent()
	if cmd == nil {
		t.Error("choosing an agent saved nothing")
	}
	if m.showAgents {
		t.Error("the picker is still open")
	}
	if m.cfg.TmuxTarget != "%10" {
		t.Errorf("tmux_target = %q, want %%10", m.cfg.TmuxTarget)
	}
	if m.cfg.FeedbackTarget != "tmux" {
		t.Errorf("feedback_target = %q — the choice would have no effect", m.cfg.FeedbackTarget)
	}
	if !strings.Contains(m.statusMsg, "claude") {
		t.Errorf("the bar does not say where feedback now goes: %q", m.statusMsg)
	}
}

// esc changes nothing.
func TestAgentPicker_CancellingWritesNothing(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	beforeTarget, beforeFeedback := m.cfg.TmuxTarget, m.cfg.FeedbackTarget
	m, _ = m.openAgentPicker()
	m = withAgents(t, m, twoAgents()...)
	m, _ = m.moveAgentCursor(1)

	m, cmd := m.cancelAgentPicker()
	if m.showAgents {
		t.Error("esc did not close the picker")
	}
	if cmd != nil {
		t.Error("esc scheduled a write")
	}
	if m.cfg.TmuxTarget != beforeTarget || m.cfg.FeedbackTarget != beforeFeedback {
		t.Errorf("esc changed the config: target=%q feedback=%q",
			m.cfg.TmuxTarget, m.cfg.FeedbackTarget)
	}
}

// With nothing running, say what was looked for. "Nothing here" and "differ is
// broken" are otherwise indistinguishable.
func TestAgentPicker_AnEmptyListSaysWhatItLookedFor(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = m.openAgentPicker()
	m = withAgents(t, m) // scanned, found none

	view := m.View()
	if !strings.Contains(view, "No agent is running") {
		t.Errorf("the picker does not say the list is empty:\n%s", view)
	}
	for _, tool := range []string{"claude", "codex", "gemini", "copilot", "opencode", "aider"} {
		if !strings.Contains(view, tool) {
			t.Errorf("the empty state does not name %q as something it looks for:\n%s", tool, view)
		}
	}
}

// A failed scan is a failure like any other, not a silently empty picker.
func TestAgentPicker_AFailedScanGoesThroughFail(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = m.openAgentPicker()

	updated, _ := m.Update(agentsLoadedMsg{err: errors.New("tmux is not installed")})
	m = updated.(Model)

	if m.showAgents {
		t.Error("the picker stayed open after the scan failed")
	}
	if m.problem == nil {
		t.Error("the failure was not retained, so ! would say nothing went wrong")
	}
}

// The picker owns the keyboard while it is open.
func TestAgentPicker_SwallowsKeysThatWouldActOnTheRepo(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	base := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	base, _ = base.openAgentPicker()
	base = withAgents(t, base, twoAgents()...)

	for _, k := range []string{"tab", "a", "v", "r", "b", "c"} {
		m := base
		updated, _ := m.Update(key(k))
		m = updated.(Model)
		if !m.showAgents {
			t.Errorf("%q closed the picker", k)
		}
		if m.mode != base.mode {
			t.Errorf("%q changed the mode to %v behind the picker", k, m.mode)
		}
		if m.splitDiff != base.splitDiff {
			t.Errorf("%q toggled split view behind the picker", k)
		}
	}
}

// A chosen pane that has gone is a choice to make again, not a message to
// read. Leaving the user to work out that the agent they picked has exited is
// the failure the picker exists to prevent.
func TestAgentPicker_APaneThatHasGoneReopensThePicker(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m.cfg.TmuxTarget = "%99"

	gone := errors.New(`tmux target "%99" does not match a pane — check tmux_target`)
	updated, cmd := m.Update(feedbackSentMsg{ids: []string{"c1"}, target: "tmux", err: gone})
	m = updated.(Model)

	if !m.showAgents {
		t.Error("a vanished pane did not reopen the picker")
	}
	if cmd == nil {
		t.Error("the picker opened without scanning")
	}
	if m.problem == nil {
		t.Error("the failure was not retained")
	}
}

// Any other send failure is reported and nothing else: reopening the picker
// for, say, a clipboard that is unavailable would be the wrong answer.
func TestAgentPicker_AnUnrelatedSendFailureDoesNotReopenIt(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})

	updated, _ := m.Update(feedbackSentMsg{
		ids: []string{"c1"}, target: "clipboard",
		err: errors.New("pbcopy: exit status 1"),
	})
	m = updated.(Model)

	if m.showAgents {
		t.Error("an unrelated failure opened the agent picker")
	}
	if m.problem == nil {
		t.Error("the failure was not retained")
	}
}

// The branch list arriving takes over the view, so it closes this picker like
// the others. Unlike the theme picker there is nothing to restore — the picker
// changes nothing until enter.
func TestAgentPicker_TheBranchListArrivingClosesIt(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = m.openAgentPicker()
	m = withAgents(t, m, twoAgents()...)

	updated, _ := m.Update(branchesLoadedMsg{branches: []string{"master"}, current: "master"})
	m = updated.(Model)

	if m.showAgents {
		t.Error("the picker survived the branch list arriving")
	}
	if strings.Contains(m.View(), "looking for agents") {
		t.Errorf("the picker is still on screen:\n%s", m.View())
	}
}
