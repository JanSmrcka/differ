package ui

import (
	"fmt"
	"strings"

	"github.com/jansmrcka/differ/internal/review"
)

// The session history overlay.
//
// Review state is session-only and deliberately so, which makes "have I
// already sent that?" unanswerable from anywhere outside the running process.
// H answers it: every attempt, what was in it, where it went and whether it
// arrived — failures included, because a send that went nowhere is the one
// worth looking up.

// renderHistoryOverlay lists the session's deliveries, most recent first.
func (m Model) renderHistoryOverlay(width, height int) string {
	rows := []string{m.styles.HelpKey.Render(" sent this session"), ""}

	sent := m.deliveries(height)
	if len(sent) == 0 {
		rows = append(rows, m.styles.HelpDesc.Render(" nothing sent yet"))
	}
	for _, d := range sent {
		rows = append(rows, m.renderDelivery(d))
	}

	rows = append(rows, "", m.styles.HelpDesc.Render(" H or esc to close"))
	return fitOverlay(rows, width, height)
}

// deliveries is as much of the history as the overlay has room for. The oldest
// entries are dropped rather than the newest, which are what the user came to
// read.
func (m Model) deliveries(height int) []review.Delivery {
	if m.session == nil {
		return nil
	}
	all := m.session.History()
	// Two header rows, two footer rows, and each entry takes one.
	if room := height - 4; room >= 0 && len(all) > room {
		all = all[:room]
	}
	return all
}

// renderDelivery is one line of the history: when, how many comments, where
// to, and what came back.
func (m Model) renderDelivery(d review.Delivery) string {
	when := d.At.Format("15:04:05")
	what := fmt.Sprintf("%s → %s", plural(len(d.Comments), "comment"), d.Target)

	outcome := m.styles.HelpDesc.Render("sent")
	if !d.OK() {
		outcome = m.styles.CommentStale.Render("failed: " + d.Err)
	}

	line := " " + m.styles.HelpDesc.Render(when) + "  " + what + "  " + outcome
	if len(d.Files) > 0 {
		line += m.styles.HelpDesc.Render("  " + strings.Join(d.Files, ", "))
	}
	return line
}
