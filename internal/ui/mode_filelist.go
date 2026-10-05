package ui

import tea "github.com/charmbracelet/bubbletea"

// File-list mode input handling and file navigation actions.

func (m Model) updateFileListMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.statusMsg = ""
	if msg.String() == "P" {
		// Disarm the other one here, not below: this block returns before
		// reaching the shared resets, so F, P, F used to pull.
		m.pullConfirm = false
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
		m.pushConfirm = false
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
	case "q":
		return m.confirmQuit()
	case "j", "down":
		if m.cursor < len(m.files)-1 {
			m.cursor++
		}
		m = m.clampFileScroll()
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		m = m.clampFileScroll()
	case "g":
		m.cursor = 0
		m = m.clampFileScroll()
	case "G":
		m.cursor = max(0, len(m.files)-1)
		m = m.clampFileScroll()
	case "enter", "l", "right":
		return m.openDiff()
	case "e":
		return m.openFileInEditor()
	case "tab":
		return m.toggleStage()
	case "a":
		return m.stageAll()
	case "C":
		return m.enterCommitMode()
	case "b":
		return m.enterBranchMode()
	case "v":
		m.splitDiff = !m.splitDiff
		m.cfg.SplitDiff = m.splitDiff
		m.prevCurs = -1
		m.lastDiffContent = ""
		return m, tea.Batch(m.loadDiffCmd(true), m.saveSplitPrefCmd())
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
		return m.clampFileScroll().onFileFocused(), m.loadDiffCmd(true)
	}
	return m, nil
}

func (m Model) prevFile() (tea.Model, tea.Cmd) {
	if m.cursor > 0 {
		m.cursor--
		m.prevCurs = m.cursor
		return m.clampFileScroll().onFileFocused(), m.loadDiffCmd(true)
	}
	return m, nil
}

// clampFileScroll brings the cursor back into the visible window, scrolling by
// the smallest amount that does it. Same shape as clampBranchScroll — the two
// lists share a panel and must behave the same way in it.
func (m Model) clampFileScroll() Model {
	h := m.listHeight()
	if h <= 0 {
		return m
	}
	switch {
	case m.cursor < m.fileOffset:
		m.fileOffset = m.cursor
	case m.cursor >= m.fileOffset+h:
		m.fileOffset = m.cursor - h + 1
	}
	// A changeset that shrank can leave the window past the end of the list.
	if maxOffset := max(len(m.files)-h, 0); m.fileOffset > maxOffset {
		m.fileOffset = maxOffset
	}
	return m
}

// onFileFocused records that a file has been looked at, but only in the diff —
// browsing the file list is not reading.
func (m Model) onFileFocused() Model {
	if m.mode == modeDiff && m.session != nil {
		m.session.MarkViewed(m.currentFilePath())
	}
	return m
}
