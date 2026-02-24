package theme

// Theme defines color values for the UI. All values are hex color strings.
// This package has no lipgloss dependency — styles.go bridges theme to lipgloss.
type Theme struct {
	Bg string
	Fg string

	// Diff colors
	AddedFg   string
	AddedBg   string
	RemovedFg string
	RemovedBg string
	HunkFg    string

	// Line numbers
	LineNumFg       string
	LineNumAddedFg  string
	LineNumRemovedFg string

	// Header bar
	HeaderBg string
	HeaderFg string

	// Hunk
	HunkBg string

	// File list
	SelectedBg string
	SelectedFg string
	StagedFg   string
	ModifiedFg string
	AddedFileFg  string
	DeletedFg  string
	RenamedFg  string
	UntrackedFg string

	// Card
	CardBg string

	// Chrome
	BorderFg    string
	StatusBarBg string
	StatusBarFg string
	HelpKeyFg   string
	HelpDescFg  string

	// Accent
	AccentFg string

	// Chroma syntax theme name
	ChromaStyle string
}

// Themes is the registry of built-in themes.
var Themes = map[string]Theme{
	"dark":  DarkTheme(),
	"light": LightTheme(),
}

// DarkTheme returns a Catppuccin Mocha-inspired pastel dark theme.
func DarkTheme() Theme {
	return Theme{
		Bg: "#1e1e2e",
		Fg: "#e0e0f0",

		AddedFg:   "#a6e3a1",
		AddedBg:   "#1e3a2c",
		RemovedFg: "#f38ba8",
		RemovedBg: "#3b1d2e",
		HunkFg:    "#6c5ce7",

		LineNumFg:        "#585b70",
		LineNumAddedFg:   "#a6e3a1",
		LineNumRemovedFg: "#f38ba8",

		HeaderBg: "#282a3a",
		HeaderFg: "#c678dd",

		HunkBg: "#252636",

		CardBg: "#232336",

		SelectedBg:  "#3d2b5a",
		SelectedFg:  "#c678dd",
		StagedFg:    "#50fa7b",
		ModifiedFg:  "#fab387",
		AddedFileFg: "#a6e3a1",
		DeletedFg:   "#f38ba8",
		RenamedFg:   "#cba6f7",
		UntrackedFg: "#7f849c",

		BorderFg:    "#6c5ce7",
		StatusBarBg: "#1a1a2e",
		StatusBarFg: "#b4befe",
		HelpKeyFg:   "#c678dd",
		HelpDescFg:  "#9399b2",

		AccentFg: "#c678dd",

		ChromaStyle: "catppuccin-mocha",
	}
}

// LightTheme returns a Catppuccin Latte-inspired pastel light theme.
func LightTheme() Theme {
	return Theme{
		Bg: "#eff1f5",
		Fg: "#4c4f69",

		AddedFg:   "#1a7f2a",
		AddedBg:   "#e6f5e4",
		RemovedFg: "#d20f39",
		RemovedBg: "#fde4e8",
		HunkFg:    "#1e66f5",

		LineNumFg:        "#9ca0b0",
		LineNumAddedFg:   "#1a7f2a",
		LineNumRemovedFg: "#d20f39",

		HeaderBg: "#e6e9ef",
		HeaderFg: "#8839ef",

		HunkBg: "#e6e9ef",

		CardBg: "#e6e9ef",

		SelectedBg:  "#d4c4f0",
		SelectedFg:  "#4c4f69",
		StagedFg:    "#087f23",
		ModifiedFg:  "#fe640b",
		AddedFileFg: "#1a7f2a",
		DeletedFg:   "#d20f39",
		RenamedFg:   "#8839ef",
		UntrackedFg: "#8c8fa1",

		BorderFg:    "#8839ef",
		StatusBarBg: "#e6e9ef",
		StatusBarFg: "#6c6f85",
		HelpKeyFg:   "#8839ef",
		HelpDescFg:  "#8c8fa1",

		AccentFg: "#8839ef",

		ChromaStyle: "catppuccin-latte",
	}
}
