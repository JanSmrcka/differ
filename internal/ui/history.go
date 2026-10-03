package ui

import (
	"fmt"
	"strings"

	"github.com/jansmrcka/differ/internal/review"
)

// The session history overlay.
//
// "Have I already sent that?" is unanswerable from anywhere but here: nothing
// differ sends leaves a mark on the repository. H answers it: every attempt,
// what was in it, where it went and whether it arrived — failures included,
// because a send that went nowhere is the one worth looking up.
//
// It outlives the process. The record of what the agent has been told is
// exactly what stops the same review going out twice, so it is written to disk
// and restored unconditionally, whatever has happened to the files since.

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
	// Not "sent this session" any more: the list outlives the session, and a
	// title claiming otherwise would undersell the one thing it is for.
	return m.fitOverlay(" already sent", rows, "H or esc to close", width, height)
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
		rows = append(rows, indent+m.styles.HelpDesc.Render(fileSummary(d.Files, max(width-len(indent)-2, 10))))
	}
	return rows
}

// fileSummary lists as many names as fit and counts the rest.
//
// Sharing the room out between them was the obvious thing and the wrong one: a
// delivery covering sixty files gave each a budget of zero columns, and the
// row came out as a line of bare commas. A name is worth nothing shortened
// past recognition, so the ones that do not fit are counted instead.
func fileSummary(files []string, room int) string {
	// Below this a path is shortened past recognition, so it is worth less
	// than the count it would displace.
	const minName = 10
	// ", +57 more"
	const tailRoom = 10

	// How many names can have a readable share of the room? Sharing it out
	// between all of them was the obvious thing and the wrong one: sixty files
	// gave each a budget of zero columns, and truncatePath returns "" for
	// that, so the row came out as a line of bare commas.
	fit, share := len(files), 0
	for ; fit > 1; fit-- {
		overhead := 2 * (fit - 1)
		if fit < len(files) {
			overhead += tailRoom
		}
		if share = (room - overhead) / fit; share >= minName {
			break
		}
	}
	if fit == 1 {
		share = room
		if len(files) > 1 {
			share -= tailRoom
		}
	}
	if share < minName {
		return plural(len(files), "file")
	}

	shown := make([]string, 0, fit)
	for _, f := range files[:fit] {
		shown = append(shown, truncatePath(f, share))
	}
	out := strings.Join(shown, ", ")
	if rest := len(files) - fit; rest > 0 {
		out += fmt.Sprintf(", +%d more", rest)
	}
	return out
}
