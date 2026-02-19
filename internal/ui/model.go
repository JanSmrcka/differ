package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/theme"
)

// Model is the main Bubble Tea model for the diff viewer.
type Model struct {
	repo       *git.Repo
	files      []fileItem
	styles     Styles
	theme      theme.Theme
	stagedOnly bool
	ref        string
	cursor     int
	width      int
	height     int
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
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

func (m Model) View() string {
	return "differ TUI — press q to quit\n"
}
