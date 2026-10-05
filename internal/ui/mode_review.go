package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/review"
)

// Review state as the diff shows it: progress, the reload after the file
// moved, and opening a file to read it. The keys are in mode_diff.go.

// openDiff moves from the file list into the file's diff, recording the visit.
func (m Model) openDiff() (tea.Model, tea.Cmd) {
	if m.session == nil {
		m.session = review.NewSession()
	}
	m.mode = modeDiff
	if len(m.files) == 0 {
		// Nothing to read, but the panel says that better than the status
		// bar can — and it says it in the same place the file list would.
		return m, nil
	}
	m.session.MarkViewed(m.currentFilePath())

	// Coming from the file list there may be no diff loaded for this file yet.
	if m.renderer == nil {
		return m, m.loadDiffCmd(true)
	}
	return m, nil
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

// reloadKey re-reads the file on screen after it moved underneath.
//
// Capital, because a reload is the one key here that replaces what is on
// screen.
const reloadKey = "R"

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

// reloadDiff re-reads the file on screen after it moved underneath.
//
// It keeps the reviewer's place: loadDiffCmd(false) clamps rather than resets,
// and handleDiffLoaded re-anchors this file's comments against the diff that
// arrives, so a comment follows its line or is marked stale rather than left
// pointing at whatever now occupies its old line number.
func (m Model) reloadDiff() (tea.Model, tea.Cmd) {
	if !m.diffStale() {
		return m, nil
	}
	// The flag is cleared by handleDiffLoaded when the reload lands, not here:
	// clearing it now would drop the notice before the diff it describes has
	// actually been replaced.
	//
	// No need to clear lastDiffContent either — it is compared against what
	// the reload renders, so content that really differs updates the viewport
	// and content that does not needs no update.
	return m, m.loadDiffCmd(false)
}
