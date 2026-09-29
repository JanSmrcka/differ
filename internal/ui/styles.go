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
	DiffAdded     lipgloss.Style
	DiffRemoved   lipgloss.Style
	DiffAddedBg   lipgloss.Style // bg-only, for padding highlighted lines
	DiffRemovedBg lipgloss.Style // bg-only, for padding highlighted lines
	// The part of a split-view line that differs from its pair. Underlined as
	// well as shaded, because under --no-color the shade is all there is and
	// a within-line distinction has no other channel.
	DiffAddedEmph      lipgloss.Style
	DiffRemovedEmph    lipgloss.Style
	DiffContext        lipgloss.Style
	DiffHunkHeader     lipgloss.Style
	DiffLineNum        lipgloss.Style
	DiffLineNumAdded   lipgloss.Style
	DiffLineNumRemoved lipgloss.Style
	// The marks differ adds inside the code column — trailing whitespace, and
	// the sign that a line was cut. One variant per line background, so the
	// mark sits on the line rather than over it.
	DiffMark        lipgloss.Style
	DiffMarkAdded   lipgloss.Style
	DiffMarkRemoved lipgloss.Style

	// Chrome — dim structure, so content stands out against it.
	Chrome          lipgloss.Style
	PanelLabel      lipgloss.Style
	PanelLabelFocus lipgloss.Style
	HeaderName      lipgloss.Style
	HeaderBranch    lipgloss.Style
	HeaderMeta      lipgloss.Style
	StatusText      lipgloss.Style

	HeaderBar lipgloss.Style
	StatusBar lipgloss.Style
	HelpKey   lipgloss.Style
	HelpDesc  lipgloss.Style
	CardBg    lipgloss.Style

	// Review comments
	CommentBar   lipgloss.Style
	CommentMeta  lipgloss.Style
	CommentBody  lipgloss.Style
	CommentStale lipgloss.Style

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
		DiffAddedEmph: lipgloss.NewStyle().
			Background(lipgloss.Color(t.AddedEmphBg)).
			Underline(true),
		DiffRemovedEmph: lipgloss.NewStyle().
			Background(lipgloss.Color(t.RemovedEmphBg)).
			Underline(true),
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
		DiffMark: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.MarkFg)),
		DiffMarkAdded: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.MarkFg)).
			Background(lipgloss.Color(t.AddedBg)),
		DiffMarkRemoved: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.MarkFg)).
			Background(lipgloss.Color(t.RemovedBg)),

		Chrome: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.ChromeFg)),
		PanelLabel: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.PanelLabelFg)),
		PanelLabelFocus: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.AccentFg)).
			Bold(true),
		HeaderName: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.HeaderNameFg)).
			Bold(true),
		HeaderBranch: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.HeaderBranchFg)),
		HeaderMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.HeaderMetaFg)),
		StatusText: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.StatusBarFg)),

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

		CommentBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.CommentFg)).
			Bold(true),
		CommentMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.CommentMetaFg)),
		CommentBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.CommentFg)),
		CommentStale: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.StaleFg)).
			Bold(true),

		CommitInput: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Fg)),

		Accent: lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.AccentFg)),
	}
}
