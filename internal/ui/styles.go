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
	DiffAdded           lipgloss.Style
	DiffRemoved         lipgloss.Style
	DiffAddedBg         lipgloss.Style // bg-only, for padding highlighted lines
	DiffRemovedBg       lipgloss.Style // bg-only, for padding highlighted lines
	DiffContext         lipgloss.Style
	DiffHunkHeader      lipgloss.Style
	DiffLineNum         lipgloss.Style
	DiffLineNumAdded    lipgloss.Style
	DiffLineNumRemoved  lipgloss.Style

	// Chrome
	HeaderBar   lipgloss.Style
	StatusBar   lipgloss.Style
	HelpKey  lipgloss.Style
	HelpDesc lipgloss.Style
	CardBg   lipgloss.Style

	// Commit input
	CommitInput lipgloss.Style

	// Accent
	Accent lipgloss.Style
}

// NewStyles creates styles from a theme.
func NewStyles(t theme.Theme) Styles {
	return Styles{
		FileItem: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Fg)).
			PaddingLeft(1),
		FileSelected: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.SelectedFg)).
			Bold(true).
			PaddingLeft(1),
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
		DiffAddedBg: lipgloss.NewStyle().
			Background(lipgloss.Color(t.AddedBg)),
		DiffRemovedBg: lipgloss.NewStyle().
			Background(lipgloss.Color(t.RemovedBg)),
		DiffContext: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Fg)),
		DiffHunkHeader: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.HunkFg)),
		DiffLineNum: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.LineNumFg)),
		DiffLineNumAdded: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.LineNumAddedFg)).
			Background(lipgloss.Color(t.AddedBg)),
		DiffLineNumRemoved: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.LineNumRemovedFg)).
			Background(lipgloss.Color(t.RemovedBg)),

		HeaderBar: lipgloss.NewStyle().
			Background(lipgloss.Color(t.HeaderBg)).
			Foreground(lipgloss.Color(t.HeaderFg)).
			Bold(true).
			PaddingLeft(1).
			PaddingRight(1),
		StatusBar: lipgloss.NewStyle().
			Background(lipgloss.Color(t.StatusBarBg)).
			Foreground(lipgloss.Color(t.StatusBarFg)),
		HelpKey: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.HelpKeyFg)).
			Bold(true).
			Underline(true),
		HelpDesc: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.HelpDescFg)),
		CardBg: lipgloss.NewStyle().
			Background(lipgloss.Color(t.CardBg)),

		CommitInput: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Fg)),

		Accent: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.AccentFg)),
	}
}
