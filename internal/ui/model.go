package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/theme"
)

type viewMode int

const (
	modeFileList viewMode = iota
	modeDiff
	modeCommit
)

const fileListWidth = 35

// Model is the main Bubble Tea model for the diff viewer.
type Model struct {
	repo       *git.Repo
	files      []fileItem
	styles     Styles
	theme      theme.Theme
	stagedOnly bool
	ref        string

	mode   viewMode
	cursor int
	width  int
	height int
}

type fileItem struct {
	change    git.FileChange
	untracked bool
}

// NewModel creates the main diff viewer model.
func NewModel(
	repo *git.Repo,
	changes []git.FileChange,
	untracked []string,
	styles Styles,
	t theme.Theme,
	stagedOnly bool,
	ref string,
) Model {
	var files []fileItem
	for _, c := range changes {
		files = append(files, fileItem{change: c})
	}
	for _, path := range untracked {
		files = append(files, fileItem{
			change:    git.FileChange{Path: path, Status: git.StatusUntracked},
			untracked: true,
		})
	}
	return Model{
		repo:       repo,
		files:      files,
		styles:     styles,
		theme:      t,
		stagedOnly: stagedOnly,
		ref:        ref,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.updateFileList(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

func (m Model) updateFileList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	// Main area height = total - status bar (1) - help bar (1)
	mainH := m.height - 2

	fileList := m.renderFileList(mainH)
	statusBar := m.renderStatusBar()
	helpBar := m.renderHelpBar()

	main := lipgloss.NewStyle().Width(m.width).Height(mainH).Render(fileList)
	return lipgloss.JoinVertical(lipgloss.Left, main, statusBar, helpBar)
}

func (m Model) renderFileList(height int) string {
	var b strings.Builder
	for i, f := range m.files {
		if i >= height {
			break
		}
		line := m.renderFileItem(f, i == m.cursor)
		b.WriteString(line)
		if i < len(m.files)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m Model) renderFileItem(f fileItem, selected bool) string {
	status := statusIcon(f.change.Status)
	staged := "  "
	if f.change.Staged {
		staged = m.styles.StagedIcon.Render("● ")
	}

	statusStyled := m.styleStatus(status, f.change.Status)
	name := f.change.Path
	if f.change.OldPath != "" {
		name = f.change.OldPath + " → " + f.change.Path
	}

	line := fmt.Sprintf("%s%s %s", staged, statusStyled, name)
	if selected {
		return m.styles.FileSelected.Width(fileListWidth).Render(line)
	}
	return m.styles.FileItem.Width(fileListWidth).Render(line)
}

func (m Model) styleStatus(icon string, status git.FileStatus) string {
	switch status {
	case git.StatusModified:
		return m.styles.StatusModified.Render(icon)
	case git.StatusAdded:
		return m.styles.StatusAdded.Render(icon)
	case git.StatusDeleted:
		return m.styles.StatusDeleted.Render(icon)
	case git.StatusRenamed:
		return m.styles.StatusRenamed.Render(icon)
	case git.StatusUntracked:
		return m.styles.StatusUntracked.Render(icon)
	default:
		return icon
	}
}

func statusIcon(s git.FileStatus) string {
	return string(s)
}

func (m Model) renderStatusBar() string {
	branch := m.repo.BranchName()
	info := fmt.Sprintf(" ⎇ %s", branch)

	fileInfo := ""
	if len(m.files) > 0 && m.cursor < len(m.files) {
		f := m.files[m.cursor]
		tag := ""
		if f.change.Staged {
			tag = " [staged]"
		}
		fileInfo = fmt.Sprintf("  %s%s", f.change.Path, tag)
	}

	right := fmt.Sprintf("%d files ", len(m.files))
	gap := m.width - lipgloss.Width(info) - lipgloss.Width(fileInfo) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	bar := info + fileInfo + strings.Repeat(" ", gap) + right
	return m.styles.StatusBar.Width(m.width).Render(bar)
}

func (m Model) renderHelpBar() string {
	pairs := []struct{ key, desc string }{
		{"j/k", "navigate"},
		{"q", "quit"},
	}

	var parts []string
	for _, p := range pairs {
		parts = append(parts,
			m.styles.HelpKey.Render(p.key)+" "+m.styles.HelpDesc.Render(p.desc))
	}
	bar := " " + strings.Join(parts, "  ·  ")
	return lipgloss.NewStyle().Width(m.width).Render(bar)
}
