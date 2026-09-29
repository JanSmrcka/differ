package ui

import tea "github.com/charmbracelet/bubbletea"

// Diff mode key handling. Navigation moves a line cursor rather than scrolling
// the viewport directly: the cursor is what review comments anchor to, so it
// has to be the thing the user drives.

func (m Model) updateDiffMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "q" {
		m.quitConfirm = false
	}
	switch msg.String() {
	case "q":
		return m.confirmQuit()
	case "esc", "h", "left":
		m.mode = modeFileList
		return m, nil
	case "r":
		return m.enterReviewMode()
	}
	return m.diffNavigation(msg)
}

// diffNavigation handles movement and the actions shared by diff and review
// mode, so the two never drift apart.
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
		m.prevCurs = -1
		m.lastDiffContent = ""
		// Reload without resetting: the cursor addresses source lines, so it
		// means the same thing in both views.
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
