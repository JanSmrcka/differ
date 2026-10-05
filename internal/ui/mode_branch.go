package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Branch picker/create mode state transitions and actions.

func (m Model) activeBranches() []string {
	if m.filteredBranches != nil {
		return m.filteredBranches
	}
	return m.branches
}

func filterBranches(branches []string, query string) []string {
	if query == "" {
		return nil
	}
	q := strings.ToLower(query)
	out := []string{}
	for _, b := range branches {
		if strings.Contains(strings.ToLower(b), q) {
			out = append(out, b)
		}
	}
	return out
}

func (m Model) enterBranchMode() (tea.Model, tea.Cmd) {
	repo := m.repo
	return m, func() tea.Msg {
		branches, err := repo.ListBranches()
		if err != nil {
			return branchesLoadedMsg{err: err}
		}
		current := repo.BranchName()
		// A failure here only costs the warning; git still refuses a switch
		// that would conflict.
		dirty, _ := repo.HasTrackedChanges()
		return branchesLoadedMsg{branches: branches, current: current, dirty: dirty}
	}
}

func (m Model) updateBranchMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.branchCreating {
		return m.updateBranchCreateMode(msg)
	}
	// The warning was about one branch: anything but enter disarms it.
	if msg.String() != "enter" {
		m.branchConfirm = ""
	}
	switch msg.String() {
	case "ctrl+n":
		m.branchCreating = true
		m.branchInput.Reset()
		m.branchInput.Focus()
		m.branchFilter.Blur()
		return m, textinput.Blink
	case "esc":
		if m.branchFilter.Value() != "" {
			m.branchFilter.Reset()
			m.filteredBranches = nil
			m.branchCursor = 0
			m.branchOffset = 0
			return m, nil
		}
		m.mode = modeFileList
		m.branchFilter.Blur()
		return m, nil
	case "up", "ctrl+k":
		if m.branchCursor > 0 {
			m.branchCursor--
		}
		m = m.clampBranchScroll()
		return m, nil
	case "down", "ctrl+j":
		list := m.activeBranches()
		if m.branchCursor < len(list)-1 {
			m.branchCursor++
		}
		m = m.clampBranchScroll()
		return m, nil
	case "enter":
		list := m.activeBranches()
		if m.branchCursor >= len(list) || len(list) == 0 {
			return m, nil
		}
		selected := list[m.branchCursor]
		m.branchFilter.Blur()
		if selected == m.currentBranch {
			m.mode = modeFileList
			return m, nil
		}
		if m.branchDirty && m.branchConfirm != selected {
			m.branchConfirm = selected
			m.statusMsg = "uncommitted changes will move to " + selected + " — enter again to switch"
			return m, nil
		}
		m.branchConfirm = ""
		repo := m.repo
		return m, func() tea.Msg {
			return branchSwitchedMsg{err: repo.CheckoutBranch(selected)}
		}
	}
	prevVal := m.branchFilter.Value()
	var cmd tea.Cmd
	m.branchFilter, cmd = m.branchFilter.Update(msg)
	if m.branchFilter.Value() != prevVal {
		m.filteredBranches = filterBranches(m.branches, m.branchFilter.Value())
		m.branchCursor = 0
		m.branchOffset = 0
	}
	return m, cmd
}

func (m Model) updateBranchCreateMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.branchCreating = false
		m.branchInput.Reset()
		m.branchFilter.Focus()
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.branchInput.Value())
		if name == "" {
			m.statusMsg = "empty branch name"
			return m, nil
		}
		return m, m.createBranchCmd(name)
	}
	var cmd tea.Cmd
	m.branchInput, cmd = m.branchInput.Update(msg)
	return m, cmd
}

func (m Model) clampBranchScroll() Model {
	// One row of the list goes to the filter bar.
	h := m.listHeight() - 1
	if h <= 0 {
		return m
	}
	if m.branchCursor < m.branchOffset {
		m.branchOffset = m.branchCursor
	} else if m.branchCursor >= m.branchOffset+h {
		m.branchOffset = m.branchCursor - h + 1
	}
	// And back off the end of the list, which is the half that matters when
	// the terminal grows: without it the window stays where it was and the
	// branches above it are unreachable until the user scrolls up.
	if maxOffset := max(len(m.activeBranches())-h, 0); m.branchOffset > maxOffset {
		m.branchOffset = maxOffset
	}
	return m
}

func (m Model) createBranchCmd(name string) tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		if err := repo.CreateBranch(name); err != nil {
			return branchCreatedMsg{name: name, err: err}
		}
		err := repo.CheckoutBranch(name)
		return branchCreatedMsg{name: name, err: err}
	}
}
