package ui

import tea "github.com/charmbracelet/bubbletea"

// Diff mode key handling. Navigation moves a line cursor rather than scrolling
// the viewport directly: the cursor is what review comments anchor to, so it
// has to be the thing the user drives.
//
// Review is not a separate mode. It was, and it put a keypress between
// reading a line and commenting on it, and gave `r` three meanings. The diff
// is where you read, so it is where you comment; nothing here touches git —
// review state lives entirely in the session.

func (m Model) updateDiffMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case "q":
		return m.confirmQuit()
	case "esc", "h", "left":
		m.mode = modeFileList
		return m, nil
	case "c":
		return m.startComment()
	case "C":
		return m.startHunkComment()
	case "x":
		return m.deleteCommentAtCursor()
	case "s":
		return m.sendCommentAtCursor()
	case "S":
		return m.sendAllPending()
	case "R":
		return m.reloadDiff()
	}
	return m.diffNavigation(msg)
}

// diffNavigation handles movement and the shared actions.
func (m Model) diffNavigation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		return m.moveCursor(1), nil
	case "k", "up":
		return m.moveCursor(-1), nil
	case "d":
		return m.moveCursor(m.halfPage()), nil
	case "u":
		return m.moveCursor(-m.halfPage()), nil
	case "g":
		return m.setCursor(0), nil
	case "G":
		return m.setCursor(m.lastCursorLine()), nil
	case "}", "]":
		return m.nextHunk(), nil
	case "{", "[":
		return m.prevHunk(), nil
	case "n":
		return m.nextFile()
	case "p":
		return m.prevFile()
	case "e":
		return m.openFileInEditor()
	case "b":
		return m.enterBranchMode()
	case "tab":
		return m.toggleStage()
	case "v":
		m.splitDiff = !m.splitDiff
		m.cfg.SplitDiff = m.splitDiff
		m.prevCurs = -1
		m.lastDiffContent = ""
		// Re-render rather than re-read while the diff is held, for the same
		// reason a resize and a theme preview do: reading the file would swap
		// in content the reviewer has not asked for and take the notice saying
		// it moved with it. The cursor addresses source lines, so it means the
		// same thing in both views either way.
		if m.holdsTheDiff() && m.diffStale() {
			return m, tea.Batch(m.rerenderCmd(), m.saveSplitPrefCmd())
		}
		return m, tea.Batch(m.loadDiffCmd(false), m.saveSplitPrefCmd())
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m Model) halfPage() int {
	if h := m.viewport.Height / 2; h > 0 {
		return h
	}
	return 1
}

func (m Model) lastCursorLine() int {
	if m.renderer == nil {
		return 0
	}
	return m.renderer.LineCount() - 1
}
