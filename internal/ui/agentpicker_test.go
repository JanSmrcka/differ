package ui

import (
	"errors"
	"strconv"
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

// Choosing has to change where a send actually goes.
//
// confirmAgent wrote m.cfg and saved the file, and nothing re-resolved
// m.target — the feedback.Target that send() uses, built once in NewModel. So
// the bar said "sending to claude in differ:2", the config said tmux, and the
// review went to the clipboard until differ was restarted. The test that was
// here asserted on m.cfg, which is the proxy, and carried the comment
// "writing only the pane would be a picker that does nothing" while the
// picker did nothing.
func TestAgentPicker_ChoosingChangesWhereASendGoes(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})

	// A default install: no feedback_target, so the live target is the
	// clipboard.
	m.cfg.FeedbackTarget = ""
	m.target, m.targetErr = feedback.Resolve(feedback.Config{})
	if m.target == nil || m.target.Name() == "tmux" {
		t.Fatalf("expected a non-tmux target to begin with, got %v", m.target)
	}

	m, _ = m.openAgentPicker()
	m = withAgents(t, m, twoAgents()...)
	m, _ = m.confirmAgent()

	if m.target == nil {
		t.Fatal("choosing an agent left no target at all")
	}
	if m.target.Name() != "tmux" {
		t.Errorf("a send would still go to %q after choosing a tmux pane", m.target.Name())
	}
}

// And a target that cannot be resolved is reported, not silently kept.
func TestAgentPicker_AnUnresolvableChoiceSaysSo(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = m.openAgentPicker()
	m = withAgents(t, m, twoAgents()...)
	m, _ = m.confirmAgent()

	// Whatever happened, the model and what it would send agree: either a
	// working target, or a recorded reason.
	if m.target == nil && m.targetErr == nil {
		t.Error("no target and no reason why")
	}
}

// The highlighted agent has to be on screen. The list had no offset, so
// fitOverlay dropped the overflow and printed "… N more" — press j past the
// edge and nothing is highlighted anywhere, then enter chooses an agent you
// cannot see. This machine has six agents across four sessions, so a short
// terminal reaches it today.
func TestAgentPicker_TheHighlightedAgentIsAlwaysOnScreen(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")

	many := make([]feedback.Agent, 12)
	for i := range many {
		many[i] = feedback.Agent{
			Pane:    "%" + strconv.Itoa(i),
			Session: "s" + strconv.Itoa(i),
			Window:  "1",
			Tool:    "claude",
			Dir:     "/repo",
		}
	}

	for _, h := range []int{14, 20, 24, 30} {
		m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: h})
		m, _ = m.openAgentPicker()
		m = withAgents(t, m, many...)

		for cursor := 0; cursor < len(many); cursor++ {
			m.agentCursor = cursor
			view := m.View()
			want := many[cursor].Session + ":1"
			if !strings.Contains(view, want) {
				t.Errorf("h=%d cursor=%d: the highlighted agent %q is not on screen",
					h, cursor, want)
				break
			}
		}
	}
}

// A modal over an overlay is two things asking at once. The picker closes the
// others on the way up, and dropping that left the suite green.
func TestAgentPicker_ClosesTheOverlaysItCoversUp(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	base := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})

	for _, tc := range []struct {
		name string
		open func(Model) Model
		shut func(Model) bool
	}{
		{"help", func(m Model) Model { m.showHelp = true; return m }, func(m Model) bool { return m.showHelp }},
		{"history", func(m Model) Model { m.showHistory = true; return m }, func(m Model) bool { return m.showHistory }},
		{"problem", func(m Model) Model { m.showProblem = true; return m }, func(m Model) bool { return m.showProblem }},
	} {
		m, _ := tc.open(base).openAgentPicker()
		if !m.showAgents {
			t.Fatalf("%s: the picker did not open", tc.name)
		}
		if tc.shut(m) {
			t.Errorf("%s: the picker opened on top of it", tc.name)
		}
	}
}

// The scan takes up to three seconds and the user can leave before it
// answers. Neither its list nor its failure belongs to a picker that is no
// longer open — a failure took over the status bar seconds after esc.
func TestAgentPicker_AScanTheUserLeftIsIgnored(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m.showAgents = false

	updated, _ := m.Update(agentsLoadedMsg{err: errors.New("tmux list-panes: exit status 1")})
	after := updated.(Model)

	if after.problem != nil {
		t.Errorf("a scan nobody is waiting for reported %v", after.problem)
	}
	if strings.Contains(after.statusMsg, "agent") {
		t.Errorf("it took over the status bar: %q", after.statusMsg)
	}
	if len(after.agents) != 0 {
		t.Error("it installed a list into a closed picker")
	}
}

// When the picker opened because a send failed, a scan that also fails must
// not replace the stored problem: `!` would then say why tmux could not be
// listed and no longer why the review did not arrive.
func TestAgentPicker_AFailedScanKeepsTheSendFailure(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})

	gone := errors.New(`tmux target "%99" does not match a pane — check tmux_target`)
	updated, _ := m.Update(feedbackSentMsg{ids: []string{"c1"}, target: "tmux", err: gone})
	m = updated.(Model)
	sent := m.problem

	updated, _ = m.Update(agentsLoadedMsg{err: errors.New("tmux list-panes: exit status 1")})
	m = updated.(Model)

	if m.problem != sent {
		t.Errorf("the send failure was replaced by the scan's: %v", m.problem)
	}
}

// A tmux server that has gone entirely is not a pane that has gone. differ's
// own wrapper said "is not available" for any display-message failure, so a
// dead server reopened a picker whose scan then failed for the same reason.
func TestPaneIsGone_OnlyForTmuxsOwnWordsAboutAPane(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  string
		want bool
	}{
		{`tmux target "%4478" does not match a pane — check tmux_target`, true},
		{`tmux target "%1" is not available: can't find pane: %1`, true},
		{`tmux target "%1" is not available: no server running on /tmp/tmux-501/default`, false},
		{`tmux target "%1" is not available: exit status 1`, false},
		{"pbcopy: exit status 1", false},
	} {
		if got := paneIsGone(errors.New(tc.err)); got != tc.want {
			t.Errorf("paneIsGone(%q) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// enter on a picker with nothing in it must do nothing rather than index an
// empty slice. Both ends: a cursor can only be negative through a bug, and
// this guard is what stands between that bug and a panic inside a tea.Cmd.
func TestAgentPicker_ConfirmingNothingChangesNothing(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	base := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})

	for _, cursor := range []int{-1, 0, 5} {
		m := base
		m.showAgents, m.agentsScanned, m.agentCursor = true, true, cursor
		after, cmd := m.confirmAgent()

		if after.cfg.TmuxTarget != "" || after.cfg.FeedbackTarget != "" {
			t.Errorf("cursor %d: confirming an empty picker wrote %+v", cursor, after.cfg)
		}
		if cmd != nil {
			t.Errorf("cursor %d: confirming an empty picker saved the config", cursor)
		}
	}
}
