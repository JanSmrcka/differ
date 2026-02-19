package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/theme"
)

// Styles holds all lipgloss styles derived from a theme.
type Styles struct {
	// File list
	FileItem     lipgloss.Style
	FileSelected lipgloss.Style
	StagedIcon   lipgloss.Style

	// File status colors
	StatusModified  lipgloss.Style
	StatusAdded     lipgloss.Style
	StatusDeleted   lipgloss.Style
	StatusRenamed   lipgloss.Style
	StatusUntracked lipgloss.Style

	// Diff
	DiffAdded      lipgloss.Style
	DiffRemoved    lipgloss.Style
	DiffContext    lipgloss.Style
	DiffHunkHeader lipgloss.Style
	DiffFileHeader lipgloss.Style
	DiffLineNum    lipgloss.Style

	// Chrome
	StatusBar lipgloss.Style
	HelpKey   lipgloss.Style
	HelpDesc  lipgloss.Style
	Border    lipgloss.Style
	Panel     lipgloss.Style

	// Commit input
	CommitInput lipgloss.Style

	// Accent
	Accent lipgloss.Style
}

// NewStyles creates styles from a theme.
func NewStyles(t theme.Theme) Styles {
	return Styles{
		FileItem: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Fg)),
		FileSelected: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.SelectedFg)).
			Background(lipgloss.Color(t.SelectedBg)).
			Bold(true),
		StagedIcon: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.StagedFg)).
			Bold(true),

		StatusModified: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.ModifiedFg)),
		StatusAdded: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.AddedFileFg)),
		StatusDeleted: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.DeletedFg)),
		StatusRenamed: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.RenamedFg)),
		StatusUntracked: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.UntrackedFg)),

		DiffAdded: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.AddedFg)).
			Background(lipgloss.Color(t.AddedBg)),
		DiffRemoved: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.RemovedFg)).
			Background(lipgloss.Color(t.RemovedBg)),
		DiffContext: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Fg)),
		DiffHunkHeader: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.HunkFg)).
			Bold(true),
		DiffFileHeader: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.AccentFg)).
			Bold(true),
		DiffLineNum: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.LineNumFg)),

		StatusBar: lipgloss.NewStyle().
			Background(lipgloss.Color(t.StatusBarBg)).
			Foreground(lipgloss.Color(t.StatusBarFg)),
		HelpKey: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.HelpKeyFg)).
			Bold(true),
		HelpDesc: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.HelpDescFg)),
		Border: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.BorderFg)),
		Panel: lipgloss.NewStyle(),

		CommitInput: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Fg)),

		Accent: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.AccentFg)),
	}
}
