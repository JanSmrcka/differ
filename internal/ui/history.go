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
	var rows []string
	if m.session == nil || len(m.session.History()) == 0 {
		rows = append(rows, m.styles.HelpDesc.Render(" nothing sent yet"))
	} else {
		for _, d := range m.session.History() {
			rows = append(rows, m.renderDelivery(d, width)...)
		}
	}
	return m.fitOverlay(" sent this session", rows, "H or esc to close", width, height)
}

// renderDelivery is one entry: when, how many comments, where to and what came
// back, then the files it covered.
//
// The files get a row of their own. Joined onto the summary they ran past the
// width of a 60-column terminal and were the first thing clipped — and they
// are what the overlay exists to answer. Each path is shortened from the
// front, which keeps the end that identifies it.
func (m Model) renderDelivery(d review.Delivery, width int) []string {
	when := m.styles.HelpDesc.Render(d.At.Format("15:04:05"))
	what := fmt.Sprintf("%s → %s", plural(len(d.Comments), "comment"), d.Target)

	outcome := m.styles.HelpDesc.Render("sent")
	if !d.OK() {
		outcome = m.styles.CommentStale.Render("failed: " + d.Err)
	}

	rows := []string{" " + when + "  " + what + "  " + outcome}
	if len(d.Files) > 0 {
		// The indent lines the paths up under the summary rather than the
		// timestamp; the rest of the row is theirs.
		const indent = "           "
		room := max(width-len(indent)-2, 10)
		short := make([]string, 0, len(d.Files))
		for _, f := range d.Files {
			short = append(short, truncatePath(f, room/max(len(d.Files), 1)))
		}
		rows = append(rows, indent+m.styles.HelpDesc.Render(strings.Join(short, ", ")))
	}
	return rows
}
