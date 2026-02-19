package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/theme"
)

// LogModel is the Bubble Tea model for the commit log browser.
type LogModel struct {
	repo   *git.Repo
	styles Styles
	theme  theme.Theme
	width  int
	height int
}

// NewLogModel creates the log browser model.
func NewLogModel(repo *git.Repo, styles Styles, t theme.Theme) LogModel {
	return LogModel{repo: repo, styles: styles, theme: t}
}

func (m LogModel) Init() tea.Cmd {
	return nil
}

func (m LogModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m LogModel) View() string {
	return "differ log — press q to quit\n"
}
