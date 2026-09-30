package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/feedback"
	"github.com/jansmrcka/differ/internal/testutil"
)

func withAgents(t *testing.T, m Model, agents ...feedback.Agent) Model {
	t.Helper()
	// The picker has to be open: a scan that lands in a closed one is
	// ignored, which is what stops a failure the user escaped out of taking
	// over the bar three seconds later.
	m.showAgents = true
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
	// clipboard — or nothing at all, on a machine with no clipboard command,
	// which is what CI is. Either way it is not tmux, which is the premise;
	// requiring a resolved target made this fail on ubuntu, where there is no
	// pbcopy and no xclip.
	m.cfg.FeedbackTarget = ""
	m.target, m.targetErr = feedback.Resolve(feedback.Config{})
	if m.target != nil && m.target.Name() == "tmux" {
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
	// And the bar still says it. Overwriting the status took away both the
	// line saying the review did not arrive and the `!` offering the rest,
	// which is the failure problem.line exists to prevent.
	if !strings.Contains(m.View(), "!") {
		t.Errorf("the bar no longer offers the details:\n%s", m.View())
	}
	if !strings.Contains(m.statusSegment(), "sending the review failed") {
		t.Errorf("the bar says %q, which does not say the review did not arrive",
			m.statusSegment())
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

// Two agents in one window share their session:window label and usually
// their directory too, so the pane id is shown to tell them apart. Nothing
// reached that branch: making labelIsAmbiguous always false left the suite
// green, and the README documents the behaviour.
func TestAgentPicker_TwoAgentsInOneWindowShowTheirPaneIds(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})

	m = withAgents(t, m,
		feedback.Agent{Pane: "%5", Session: "web", Window: "2", Tool: "opencode", Dir: "/repo"},
		feedback.Agent{Pane: "%6", Session: "web", Window: "2", Tool: "claude", Dir: "/repo"},
		feedback.Agent{Pane: "%9", Session: "solo", Window: "1", Tool: "claude", Dir: "/repo"},
	)
	m.agentsScanned = true

	view := m.View()
	for _, pane := range []string{"%5", "%6"} {
		if !strings.Contains(view, pane) {
			t.Errorf("the two agents in web:2 are not told apart — %s is absent:\n%s", pane, view)
		}
	}
	// And a label that is not ambiguous is left alone: a pane id on every
	// row is noise.
	if strings.Contains(view, "%9") {
		t.Errorf("an unambiguous label was given a pane id anyway:\n%s", view)
	}
}

// Scanning and having found none are different answers and say so — a
// picker that has not finished looking must not read as an empty tmux.
func TestAgentPicker_ScanningDoesNotLookLikeFoundNone(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	base := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})

	scanning := base
	scanning.showAgents, scanning.agentsScanned = true, false
	found := base
	found.showAgents, found.agentsScanned = true, true

	if a, b := scanning.agentClosing(), found.agentClosing(); a == b {
		t.Errorf("scanning and found-none both close with %q", a)
	}
	if a, b := scanning.View(), found.View(); a == b {
		t.Error("scanning and found-none draw the same box")
	}
	if !strings.Contains(scanning.View(), "looking") {
		t.Errorf("a scan in progress does not say it is looking:\n%s", scanning.View())
	}
}

// j and k move through the list. They are in the keymap and the bar
// advertises them; making them do nothing left the suite green.
func TestAgentPicker_JAndKMoveTheCursor(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m = withAgents(t, m, twoAgents()...)
	m.agentsScanned, m.agentCursor = true, 0

	down, _ := m.agentPickerKey("j")
	if down.agentCursor != 1 {
		t.Errorf("j left the cursor at %d, want 1", down.agentCursor)
	}
	up, _ := down.agentPickerKey("k")
	if up.agentCursor != 0 {
		t.Errorf("k left the cursor at %d, want 0", up.agentCursor)
	}
	// And it clamps rather than wrapping at both ends.
	top, _ := m.agentPickerKey("k")
	if top.agentCursor != 0 {
		t.Errorf("k at the top wrapped to %d", top.agentCursor)
	}
	bottom := down
	for range 5 {
		bottom, _ = bottom.agentPickerKey("j")
	}
	if bottom.agentCursor != len(m.agents)-1 {
		t.Errorf("j past the bottom left the cursor at %d, want %d",
			bottom.agentCursor, len(m.agents)-1)
	}
}

// A choice that cannot be resolved has to say so. The test that named this
// used a pane that resolves fine and asserted only that one of target or
// targetErr was set, which is true whatever the code does.
func TestAgentPicker_AChoiceThatCannotBeResolvedIsReported(t *testing.T) {
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m = withAgents(t, m, twoAgents()...)
	m.agentsScanned, m.agentCursor = true, 0

	// No tmux on PATH, so the pane cannot be resolved however valid it looks.
	t.Setenv("PATH", t.TempDir())

	after, cmd := m.confirmAgent()

	if after.targetErr == nil {
		t.Fatal("an unresolvable choice reported no error")
	}
	if after.problem == nil {
		t.Error("the failure did not go through fail, so `!` says nothing about it")
	}
	if cmd != nil {
		t.Error("an unresolvable choice was still saved to the config")
	}
}

// enter while the scan is still running must not throw it away. The picker
// closed first, so the list arrived a moment later, found it shut and was
// discarded: no picker, no list, and nothing on screen to say why.
func TestAgentPicker_EnterDuringTheScanWaitsForIt(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m.showAgents, m.agentsScanned = true, false

	pressed, _ := m.agentPickerKey("enter")
	if !pressed.showAgents {
		t.Fatal("enter closed a picker that had not finished looking")
	}

	// And the list, when it lands, is still installed.
	updated, _ := pressed.Update(agentsLoadedMsg{agents: twoAgents()})
	after := updated.(Model)
	if len(after.agents) != 2 {
		t.Errorf("the scan's result was discarded: %d agents", len(after.agents))
	}
}

// The README's picker is what differ draws. Its first version showed aligned
// columns and `~`-abbreviated paths, neither of which the renderer produces.
func TestAgentPicker_TheREADMEShowsWhatIsDrawn(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(raw), "### Choosing the agent")
	if !ok {
		t.Fatal("the README no longer has a section about choosing the agent")
	}
	_, block, _ := strings.Cut(after, "```\n")
	shown, _, _ := strings.Cut(block, "```")

	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := settle(t, liveModel(t, tr), tea.WindowSizeMsg{Width: 120, Height: 30})
	m = withAgents(t, m,
		feedback.Agent{Pane: "%2", Session: "differ", Window: "2", Tool: "claude", Dir: "/Users/you/git/private/differ"},
		feedback.Agent{Pane: "%4", Session: "ELI-panda", Window: "2", Tool: "claude", Dir: "/Users/you/git/work/ELI-panda"},
		feedback.Agent{Pane: "%5", Session: "personal-web", Window: "2", Tool: "opencode", Dir: "/Users/you/git/private/personal-web"},
		feedback.Agent{Pane: "%6", Session: "personal-web", Window: "2", Tool: "claude", Dir: "/Users/you/git/private/personal-web"},
	)
	m.agentsScanned = true

	// Every agent row the README shows has to appear in the box, spacing and
	// all — which is what catches a hand-tidied screenshot.
	drawn, _ := splitANSI(m.View())
	for _, line := range strings.Split(shown, "\n") {
		row := strings.TrimSpace(line)
		if row == "" || !strings.Contains(row, ":") || strings.HasPrefix(row, "j/k") {
			continue
		}
		if !strings.Contains(drawn, row) {
			t.Errorf("the README shows a row differ does not draw:\n  %q\nin:\n%s", row, drawn)
		}
	}
}
