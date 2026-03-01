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
	m.pushConfirm = false

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
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
		if m.cursor < len(m.files) {
			m.SelectedFile = m.files[m.cursor].change.Path
		}
		return m, tea.Quit
	case "tab":
		return m.toggleStage()
	case "a":
		return m.stageAll()
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
		return m, m.loadDiffCmd(true)
	}
	return m, nil
}

func (m Model) prevFile() (tea.Model, tea.Cmd) {
	if m.cursor > 0 {
		m.cursor--
		m.prevCurs = m.cursor
		return m, m.loadDiffCmd(true)
	}
	return m, nil
}
