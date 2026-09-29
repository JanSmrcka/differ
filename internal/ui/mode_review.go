package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/review"
)

// Review mode: the diff view with review affordances on top.
//
// It shares navigation with diff mode rather than duplicating it, so a cursor
// position means the same thing in both. Nothing here touches git — review
// state lives entirely in the session.

// enterReviewMode switches into review, creating the session on first use.
func (m Model) enterReviewMode() (tea.Model, tea.Cmd) {
	if len(m.files) == 0 {
		// Nothing to review, but the panel says that better than the status
		// bar can — and it says it in the same place the file list would.
		m.mode = modeReview
		return m, nil
	}
	if m.session == nil {
		m.session = review.NewSession()
	}
	m.mode = modeReview
	m.session.MarkViewed(m.currentFilePath())

	// Coming from the file list there may be no diff loaded for this file yet.
	if m.renderer == nil {
		return m, m.loadDiffCmd(true)
	}
	return m, nil
}

func (m Model) updateReviewMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.commenting {
		return m.updateCommentEditor(msg)
	}

	// Both confirmations are per action, not per session: anything other than
	// repeating the action disarms them.
	if k := msg.String(); k != "q" {
		m.quitConfirm = false
	}
	if k := msg.String(); k != "s" && k != "S" {
		m.staleConfirm = false
	}

	switch msg.String() {
	case "c":
		return m.startComment()
	case "C":
		return m.startHunkComment()
	case "x":
		return m.deleteCommentAtCursor()
	case "H":
		m.showHistory = !m.showHistory
		return m, nil
	case "s":
		return m.sendCommentAtCursor()
	case "S":
		return m.sendAllPending()
	case "esc":
		m.mode = modeFileList
		return m, nil
	case "r":
		// Toggle back to the plain diff view.
		m.mode = modeDiff
		return m, nil
	case "q":
		return m.confirmQuit()
	}
	return m.diffNavigation(msg)
}

// currentFilePath is the file the cursor is on, or "" when there is none.
func (m Model) currentFilePath() string {
	if m.cursor < 0 || m.cursor >= len(m.files) {
		return ""
	}
	return m.files[m.cursor].change.Path
}

// reviewProgress summarises the session against the files currently shown.
func (m Model) reviewProgress() review.Progress {
	if m.session == nil {
		return review.Progress{Total: len(m.files)}
	}
	paths := make([]string, 0, len(m.files))
	for _, f := range m.files {
		paths = append(paths, f.change.Path)
	}
	return m.session.Progress(paths)
}

// reviewSummary is the one-line progress readout shown while reviewing.
func (m Model) reviewSummary() string {
	p := m.reviewProgress()
	out := fmt.Sprintf("%d/%d reviewed", p.Reviewed, p.Total)
	if p.Comments > 0 {
		out += "  " + plural(p.Comments, "comment")
	}
	if p.Pending > 0 {
		out += fmt.Sprintf("  %d pending", p.Pending)
	}
	if p.Sent > 0 {
		out += fmt.Sprintf("  %d sent", p.Sent)
	}
	if p.Stale > 0 {
		out += fmt.Sprintf("  %d stale", p.Stale)
	}
	// Files the agent rewrote while the user was reading elsewhere. Worth
	// saying out loud: they no longer count as reviewed, so the ratio above
	// would otherwise appear to go backwards for no reason.
	if p.Changed > 0 {
		out += fmt.Sprintf("  %d changed", p.Changed)
	}
	return out
}
