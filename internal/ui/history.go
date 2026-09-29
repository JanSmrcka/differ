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

	if m.session == nil || len(m.session.History()) == 0 {
		rows = append(rows, m.styles.HelpDesc.Render(" nothing sent yet"))
	}
	for _, d := range m.deliveries(height) {
		rows = append(rows, m.renderDelivery(d))
	}

	rows = append(rows, "", m.styles.HelpDesc.Render(" H or esc to close"))
	for i, r := range rows {
		rows[i] = padTo(r, width)
	}
	for len(rows) < height {
		rows = append(rows, padTo("", width))
	}
	return strings.Join(rows[:max(height, 0)], "\n")
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

// noteChangedFiles tells the session which files moved since the last refresh.
//
// The file under the cursor is excluded: whatever arrives for it is what the
// user is looking at, so it cannot be out of date to its own reader. Every
// other file they had already read becomes "changed", and the file list says
// so.
func (m Model) noteChangedFiles(keys map[string]string) Model {
	if keys == nil {
		// A refresh that could not fingerprint anything says nothing about
		// what changed — better silent than wrong.
		return m
	}
	if m.session != nil && m.fileKeys != nil {
		current := m.currentFilePath()
		for path, key := range keys {
			was, known := m.fileKeys[path]
			if known && was != key && path != current {
				m.session.NoteChange(path)
			}
		}
	}
	m.fileKeys = keys
	return m
}
