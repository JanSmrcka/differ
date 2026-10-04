package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jansmrcka/differ/internal/feedback"
	"github.com/jansmrcka/differ/internal/review"
)

// watchingFake is a target that can say when the agent has finished, as the
// herdr target can.
type watchingFake struct {
	*feedback.Fake
	state string
	// block, when set, makes Wait wait for cancellation.
	block bool
}

func (w *watchingFake) Wait(ctx context.Context) (string, error) {
	if w.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return w.state, nil
}

// Under herdr a sent review is not the end of what differ knows: the agent
// picks it up, and later finishes. Both are on the delivery, in H and on the
// comments it carried, and the finish is said in the bar.
func TestAgentWait_ASentReviewFollowsTheAgent(t *testing.T) {
	t.Parallel()
	m, fake := sendModel(t)
	m.target = &watchingFake{Fake: fake, state: "done"}

	updated, cmd := m.sendAllPending()
	m = updated.(Model)
	updated, wait := m.Update(cmd())
	m = updated.(Model)

	if wait == nil {
		t.Fatal("a send to a target that can watch started no wait")
	}
	if got := m.session.History()[0].Agent; got != "working" {
		t.Errorf("after the send the agent is %q, want working", got)
	}
	ids := m.session.History()[0].Comments
	if got := m.session.AgentStateOf(ids[0]); got != "working" {
		t.Errorf("comment state = %q, want working", got)
	}
	if got := m.View(); !strings.Contains(got, "sent · agent working") {
		t.Errorf("the comment does not say the agent picked it up:\n%s", got)
	}

	updated, _ = m.Update(wait())
	m = updated.(Model)
	if got := m.session.History()[0].Agent; got != "done" {
		t.Errorf("after the wait the agent is %q, want done", got)
	}
	if !strings.Contains(m.statusMsg, "answered") {
		t.Errorf("the bar does not say the agent answered: %q", m.statusMsg)
	}
	if got := m.View(); !strings.Contains(got, "sent · agent answered") {
		t.Errorf("the comment does not say the agent answered:\n%s", got)
	}
	m.showHistory = true
	if got := m.View(); !strings.Contains(got, "agent answered") {
		t.Errorf("H does not show what the agent did:\n%s", got)
	}
}

// A blocked agent is waiting on the user, which is worth saying differently.
func TestAgentWait_ABlockedAgentIsWaitingForYou(t *testing.T) {
	t.Parallel()
	m, fake := sendModel(t)
	m.target = &watchingFake{Fake: fake, state: "blocked"}
	updated, cmd := m.sendAllPending()
	m = updated.(Model)
	updated, wait := m.Update(cmd())
	m = updated.(Model)
	updated, _ = m.Update(wait())
	m = updated.(Model)
	if !strings.Contains(m.statusMsg, "waiting for you") {
		t.Errorf("bar = %q", m.statusMsg)
	}
}

// tmux cannot be asked, and must not be made to try: the comment goes to sent
// and stops there.
func TestAgentWait_ATargetThatCannotWatchStartsNoWait(t *testing.T) {
	t.Parallel()
	m, _ := sendModel(t)
	updated, cmd := m.sendAllPending()
	m = updated.(Model)
	updated, wait := m.Update(cmd())
	m = updated.(Model)
	if wait != nil {
		t.Error("a wait was started for a target that cannot watch")
	}
	if got := m.session.History()[0].Agent; got != "" {
		t.Errorf("agent state = %q, want nothing known", got)
	}
}

// herdr rejecting the prompt because the agent is blocked is a failure with
// the reason in it, and the comments stay pending.
func TestAgentWait_ABlockedPromptLeavesTheCommentsPending(t *testing.T) {
	t.Parallel()
	m, _ := sendModel(t)
	blocked := &feedback.HerdrError{Code: "agent_blocked", Message: "agent is blocked"}
	updated, _ := m.Update(feedbackSentMsg{ids: []string{m.session.Comments()[0].ID}, target: "herdr", err: blocked})
	m = updated.(Model)

	if m.problem == nil {
		t.Fatal("the bounce did not go through fail")
	}
	if !strings.Contains(m.statusMsg, "answer it") {
		t.Errorf("the bar does not say what to do: %q", m.statusMsg)
	}
	for _, c := range m.session.Comments() {
		if c.State != review.StatePending {
			t.Errorf("comment %s is %v after a bounce", c.ID, c.State)
		}
	}
	if m.showAgents {
		t.Error("a blocked agent reopened the picker — it is still there")
	}
}

// A herdr agent that has gone reopens the picker, as a tmux pane that has
// gone does.
func TestAgentWait_AGoneHerdrAgentReopensThePicker(t *testing.T) {
	t.Parallel()
	m, _ := sendModel(t)
	gone := &feedback.HerdrError{Code: "agent_not_found", Message: "agent target w9:p9 not found"}
	updated, _ := m.Update(feedbackSentMsg{ids: []string{"c1"}, target: "herdr", err: gone})
	m = updated.(Model)
	if !m.showAgents {
		t.Error("a vanished herdr agent did not reopen the picker")
	}
}

// Quitting with a wait outstanding must not hang: Close cancels it.
func TestAgentWait_CloseCancelsTheWait(t *testing.T) {
	t.Parallel()
	m, fake := sendModel(t)
	m.target = &watchingFake{Fake: fake, block: true}
	updated, cmd := m.sendAllPending()
	m = updated.(Model)
	updated, wait := m.Update(cmd())
	m = updated.(Model)

	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()
	m.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the wait outlived Close")
	}
}
