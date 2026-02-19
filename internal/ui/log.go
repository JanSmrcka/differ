package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/theme"
)

type logMode int

const (
	logModeList logMode = iota
	logModeDiff
)

type logLoadedMsg struct {
	commits []git.Commit
}

type logDiffLoadedMsg struct {
	content string
	hash    string
}

// LogModel is the Bubble Tea model for the commit log browser.
type LogModel struct {
	repo     *git.Repo
	styles   Styles
	theme    theme.Theme
	commits  []git.Commit
	cursor   int
	mode     logMode
	viewport viewport.Model
	width    int
	height   int
	ready    bool
}

// NewLogModel creates the log browser model.
func NewLogModel(repo *git.Repo, styles Styles, t theme.Theme) LogModel {
	return LogModel{repo: repo, styles: styles, theme: t}
}

func (m LogModel) Init() tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		commits, _ := repo.Log(100)
		return logLoadedMsg{commits: commits}
	}
}

func (m LogModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport = viewport.New(m.width, m.height-2)
		m.ready = true
	case logLoadedMsg:
		m.commits = msg.commits
	case logDiffLoadedMsg:
		m.viewport.SetContent(msg.content)
		m.viewport.GotoTop()
		m.mode = logModeDiff
	case tea.KeyMsg:
		switch m.mode {
		case logModeList:
			return m.updateList(msg)
		case logModeDiff:
			return m.updateDiff(msg)
		}
	}
	return m, nil
}

func (m LogModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(m.commits)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = max(0, len(m.commits)-1)
	case "enter":
		if len(m.commits) > 0 {
			return m, m.loadCommitDiff()
		}
	}
	return m, nil
}

func (m LogModel) updateDiff(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = logModeList
		return m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m LogModel) loadCommitDiff() tea.Cmd {
	commit := m.commits[m.cursor]
	repo := m.repo
	styles := m.styles
	t := m.theme
	width := m.width

	return func() tea.Msg {
		raw, err := repo.CommitDiff(commit.Hash)
		if err != nil {
			return logDiffLoadedMsg{content: "Error: " + err.Error(), hash: commit.Hash}
		}
		// Guess filename from diff headers for syntax highlighting
		content := renderCommitDiff(raw, styles, t, width)
		return logDiffLoadedMsg{content: content, hash: commit.Hash}
	}
}

// renderCommitDiff renders a full commit diff (may contain multiple files).
func renderCommitDiff(raw string, styles Styles, t theme.Theme, width int) string {
	initChromaStyle(t.ChromaStyle)

	var b strings.Builder
	// Split by file boundaries and render each section
	currentFile := ""
	var currentLines []string

	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			// Flush previous file
			if len(currentLines) > 0 {
				parsed := ParseDiff(strings.Join(currentLines, "\n"))
				b.WriteString(RenderDiff(parsed, currentFile, styles, t, width))
			}
			currentFile = extractFilename(line)
			// Add file separator
			b.WriteString(styles.HeaderBar.Width(width).Render(" " + currentFile))
			b.WriteByte('\n')
			currentLines = []string{line}
		} else {
			currentLines = append(currentLines, line)
		}
	}
	// Flush last file
	if len(currentLines) > 0 {
		parsed := ParseDiff(strings.Join(currentLines, "\n"))
		b.WriteString(RenderDiff(parsed, currentFile, styles, t, width))
	}
	return b.String()
}

// extractFilename pulls the b/ path from "diff --git a/foo b/foo".
func extractFilename(diffHeader string) string {
	parts := strings.SplitN(diffHeader, " b/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func (m LogModel) View() string {
	if !m.ready {
		return ""
	}

	switch m.mode {
	case logModeDiff:
		return m.viewDiff()
	default:
		return m.viewList()
	}
}

func (m LogModel) viewList() string {
	mainH := m.height - 2
	var b strings.Builder
	for i, c := range m.commits {
		if i >= mainH {
			break
		}
		line := m.renderCommitLine(c, i == m.cursor)
		b.WriteString(line)
		if i < len(m.commits)-1 {
			b.WriteByte('\n')
		}
	}

	main := lipgloss.NewStyle().Width(m.width).Height(mainH).Render(b.String())
	status := m.styles.StatusBar.Width(m.width).Render(
		fmt.Sprintf(" %d commits", len(m.commits)))
	help := m.renderLogHelp(false)
	return lipgloss.JoinVertical(lipgloss.Left, main, status, help)
}

func (m LogModel) renderCommitLine(c git.Commit, selected bool) string {
	hash := m.styles.Accent.Render(c.Short)
	date := m.styles.HelpDesc.Render(c.Date)
	line := fmt.Sprintf("%s  %s  %s", hash, c.Subject, date)
	if selected {
		return m.styles.FileSelected.Width(m.width).Render(line)
	}
	return lipgloss.NewStyle().Width(m.width).Render(line)
}

func (m LogModel) viewDiff() string {
	mainH := m.height - 2
	diff := lipgloss.NewStyle().Width(m.width).Height(mainH).Render(m.viewport.View())

	c := m.commits[m.cursor]
	status := m.styles.StatusBar.Width(m.width).Render(
		fmt.Sprintf(" %s  %s — %s", c.Short, c.Subject, c.Author))
	help := m.renderLogHelp(true)
	return lipgloss.JoinVertical(lipgloss.Left, diff, status, help)
}

func (m LogModel) renderLogHelp(inDiff bool) string {
	var pairs []struct{ key, desc string }
	if inDiff {
		pairs = []struct{ key, desc string }{
			{"j/k", "scroll"},
			{"d/u", "½ page"},
			{"esc", "back"},
			{"q", "quit"},
		}
	} else {
		pairs = []struct{ key, desc string }{
			{"j/k", "navigate"},
			{"enter", "view diff"},
			{"q", "quit"},
		}
	}
	var parts []string
	for _, p := range pairs {
		parts = append(parts,
			m.styles.HelpKey.Render(p.key)+" "+m.styles.HelpDesc.Render(p.desc))
	}
	bar := " " + strings.Join(parts, "  ·  ")
	return lipgloss.NewStyle().Width(m.width).Render(bar)
}
