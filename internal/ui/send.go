package ui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/feedback"
	"github.com/jansmrcka/differ/internal/review"
)

// Sending review feedback.
//
// Delivery is asynchronous and comments are only marked sent once the target
// confirms. A failure leaves everything pending so nothing the user wrote is
// lost to a missing clipboard helper or a closed tmux pane.

// FlushFeedback writes out any feedback the target buffered during the
// session. The stdout target cannot print while the TUI owns the alternate
// screen, so the caller flushes once the program has exited.
func (m Model) FlushFeedback(w io.Writer) error {
	if f, ok := m.target.(feedback.Flusher); ok {
		return f.Flush(w)
	}
	return nil
}

// confirmQuit asks once before quitting with comments that were never sent.
// Review state is session-only, so quitting really does discard them.
func (m Model) confirmQuit() (tea.Model, tea.Cmd) {
	pending := 0
	if m.session != nil {
		pending = m.session.PendingCount()
	}
	if pending == 0 || m.quitConfirm {
		return m, tea.Quit
	}
	m.quitConfirm = true
	m.statusMsg = fmt.Sprintf("%s not sent — q again to discard, S to send", plural(pending, "comment"))
	return m, nil
}

// sendCommentAtCursor delivers just the comment under the cursor.
func (m Model) sendCommentAtCursor() (tea.Model, tea.Cmd) {
	c, ok := m.commentAtCursor()
	if !ok {
		m.statusMsg = "no comment here to send"
		return m, nil
	}
	if c.WasSent() {
		m.statusMsg = "already sent"
		return m, nil
	}
	if c.State == review.StateStale && !m.staleConfirm {
		m.staleConfirm = true
		m.statusMsg = "comment is stale — s again to send anyway"
		return m, nil
	}
	return m.send([]review.Comment{c})
}

// sendAllPending delivers every comment that has not gone out yet.
//
// Healthy comments go immediately. Stale ones — whose code has changed since
// they were written — need a second, explicit press, so feedback about code
// that no longer exists is never sent by accident.
func (m Model) sendAllPending() (tea.Model, tea.Cmd) {
	if m.session == nil {
		m.statusMsg = "no comments to send"
		return m, nil
	}

	var pending, stale []review.Comment
	for _, c := range m.session.Comments() {
		// Anything already delivered is history, even if it later went stale
		// because its file left the diff. Re-sending would hand the agent the
		// whole review a second time.
		if c.WasSent() {
			continue
		}
		switch c.State {
		case review.StatePending:
			pending = append(pending, c)
		case review.StateStale:
			stale = append(stale, c)
		}
	}

	switch {
	case len(pending) > 0:
		updated, cmd := m.send(pending)
		if len(stale) > 0 {
			// Arm the guard now, so the promised "S again" actually sends them
			// rather than only re-prompting.
			mm := updated.(Model)
			mm.staleConfirm = true
			mm.statusMsg += fmt.Sprintf(" · %s stale, S again to send", plural(len(stale), "comment"))
			return mm, cmd
		}
		return updated, cmd
	case len(stale) == 0:
		m.statusMsg = "no pending comments to send"
		return m, nil
	case !m.staleConfirm:
		m.staleConfirm = true
		m.statusMsg = fmt.Sprintf("%s stale — S again to send anyway", plural(len(stale), "comment"))
		return m, nil
	default:
		return m.send(stale)
	}
}

func (m Model) send(cs []review.Comment) (tea.Model, tea.Cmd) {
	if m.target == nil {
		m.statusMsg = m.targetProblem()
		return m, nil
	}

	payload := review.FormatFeedback(cs)
	ids := make([]string, 0, len(cs))
	for _, c := range cs {
		ids = append(ids, c.ID)
	}

	target := m.target
	m.statusMsg = fmt.Sprintf("sending %s…", plural(len(cs), "comment"))
	return m, func() tea.Msg {
		err := target.Send(context.Background(), payload)
		return feedbackSentMsg{ids: ids, target: target.Name(), err: err}
	}
}

func (m Model) handleFeedbackSent(msg feedbackSentMsg) (tea.Model, tea.Cmd) {
	m.recordDelivery(msg)
	if msg.err != nil {
		// Comments stay pending: the user can retry or switch target.
		m = m.fail("send", msg.err)
		// A pane that has gone is not a failure to read about, it is a choice
		// to make again — so the picker opens rather than leaving the user to
		// work out that the agent they chose has exited.
		if paneIsGone(msg.err) {
			mm, cmd := m.openAgentPicker()
			return mm, cmd
		}
		return m, nil
	}
	if m.session != nil {
		m.session.MarkSent(msg.ids)
	}
	m.statusMsg = fmt.Sprintf("sent %s to %s", plural(len(msg.ids), "comment"), msg.target)
	return m.refreshCommentMarks(), nil
}

// targetProblem explains why there is nowhere to send to.
func (m Model) targetProblem() string {
	if m.targetErr != nil {
		return m.targetErr.Error()
	}
	return "no feedback target configured"
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// recordDelivery files an attempt in the session history, whether it worked or
// not. It is called before anything is marked sent, so the history is written
// even when the delivery failed — a send that silently went nowhere is the one
// the user most needs to be able to look up.
func (m Model) recordDelivery(msg feedbackSentMsg) {
	if m.session == nil || len(msg.ids) == 0 {
		return
	}
	d := review.Delivery{
		At:       time.Now(),
		Target:   msg.target,
		Comments: msg.ids,
		Files:    m.filesOf(msg.ids),
	}
	if msg.err != nil {
		d.Err = msg.err.Error()
	}
	m.session.RecordDelivery(d)
}

// filesOf names the files a set of comments came from, each once, in the order
// the comments were sent.
func (m Model) filesOf(ids []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range ids {
		c, ok := m.session.Get(id)
		if !ok || seen[c.File] {
			continue
		}
		seen[c.File] = true
		out = append(out, c.File)
	}
	return out
}

// paneIsGone reports whether a delivery failed because the chosen pane no
// longer exists.
//
// Matched on tmux's wording, the same way the git hints are: tmux has no exit
// code for it, and an unmatched failure still gets reported — it just does not
// reopen the picker.
func paneIsGone(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, said := range []string{
		"can't find pane",
		"pane not found",
		"does not match a pane",
		"is not available",
		"no such pane",
	} {
		if strings.Contains(text, said) {
			return true
		}
	}
	return false
}
