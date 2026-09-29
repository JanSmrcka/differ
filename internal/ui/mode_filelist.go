package ui

import tea "github.com/charmbracelet/bubbletea"

// File-list mode input handling and file navigation actions.

func (m Model) updateFileListMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.statusMsg = ""
	if msg.String() == "P" {
		if m.pushConfirm {
			m.pushConfirm = false
			m.statusMsg = "pushing..."
			if m.upstream.Upstream == "" {
				return m, m.pushSetUpstreamCmd()
			}
			return m, m.pushCmd()
		}
		if m.upstream.Upstream == "" {
			branch := m.currentBranch
			if branch == "" {
				branch = m.repo.BranchName()
			}
			m.pushConfirm = true
			m.statusMsg = "press P again to push --set-upstream origin " + branch
			return m, nil
		}
		m.pushConfirm = true
		m.statusMsg = "press P again to push to " + m.upstream.Upstream
		return m, nil
	}
	// Pull reaches the remote and rewrites the working tree, so it asks the
	// same way push does. It used to go straight through, while the help said
	// it would ask — the keymap now marks both, and a test holds them to it.
	if msg.String() == "F" {
		if m.pullConfirm {
			m.pullConfirm = false
			m.statusMsg = "pulling..."
			return m, m.pullCmd()
		}
		m.pullConfirm = true
		target := m.upstream.Upstream
		if target == "" {
			target = "the upstream branch"
		}
		m.statusMsg = "press F again to pull from " + target
		return m, nil
	}

	m.pushConfirm = false
	m.pullConfirm = false
	if msg.String() != "q" {
		m.quitConfirm = false
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		return m.confirmQuit()
	case "j", "down":
		if m.cursor < len(m.files)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = max(0, len(m.files)-1)
	case "enter", "l", "right":
		m.mode = modeDiff
		return m, nil
	case "e":
		return m.openFileInEditor()
	case "tab":
		return m.toggleStage()
	case "a":
		return m.stageAll()
	case "r":
		return m.enterReviewMode()
	case "c":
		return m.enterCommitMode()
	case "b":
		return m.enterBranchMode()
	case "v":
		m.splitDiff = !m.splitDiff
		m.prevCurs = -1
		m.lastDiffContent = ""
		return m, tea.Batch(m.loadDiffCmd(true), m.saveSplitPrefCmd())
	case "F":
		if m.upstream.Upstream == "" {
			m.statusMsg = "no upstream configured"
			return m, nil
		}
		m.statusMsg = "pulling..."
		return m, m.pullCmd()
	}
	if m.cursor != m.prevCurs {
		m.prevCurs = m.cursor
		return m, m.loadDiffCmd(true)
	}
	return m, nil
}

func (m Model) nextFile() (tea.Model, tea.Cmd) {
	if m.cursor < len(m.files)-1 {
		m.cursor++
		m.prevCurs = m.cursor
		return m.onFileFocused(), m.loadDiffCmd(true)
	}
	return m, nil
}

func (m Model) prevFile() (tea.Model, tea.Cmd) {
	if m.cursor > 0 {
		m.cursor--
		m.prevCurs = m.cursor
		return m.onFileFocused(), m.loadDiffCmd(true)
	}
	return m, nil
}

// onFileFocused records that a file has been looked at, but only while
// reviewing — browsing the file list is not reviewing.
func (m Model) onFileFocused() Model {
	if m.mode == modeReview && m.session != nil {
		m.session.MarkViewed(m.currentFilePath())
	}
	return m
}
