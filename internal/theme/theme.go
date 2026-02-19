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
	LineNumFg string

	// File list
	SelectedBg string
	SelectedFg string
	StagedFg   string
	ModifiedFg string
	AddedFileFg  string
	DeletedFg  string
	RenamedFg  string
	UntrackedFg string

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

// DarkTheme returns a GitHub Dark-inspired theme.
func DarkTheme() Theme {
	return Theme{
		Bg: "#0d1117",
		Fg: "#c9d1d9",

		AddedFg:   "#3fb950",
		AddedBg:   "#12261e",
		RemovedFg: "#f85149",
		RemovedBg: "#2d1214",
		HunkFg:    "#58a6ff",

		LineNumFg: "#484f58",

		SelectedBg: "#161b22",
		SelectedFg: "#f0f6fc",
		StagedFg:   "#3fb950",
		ModifiedFg: "#d29922",
		AddedFileFg:  "#3fb950",
		DeletedFg:  "#f85149",
		RenamedFg:  "#d2a8ff",
		UntrackedFg: "#8b949e",

		BorderFg:    "#30363d",
		StatusBarBg: "#161b22",
		StatusBarFg: "#8b949e",
		HelpKeyFg:   "#58a6ff",
		HelpDescFg:  "#8b949e",

		AccentFg: "#58a6ff",

		ChromaStyle: "github-dark",
	}
}

// LightTheme returns a GitHub Light-inspired theme.
func LightTheme() Theme {
	return Theme{
		Bg: "#ffffff",
		Fg: "#1f2328",

		AddedFg:   "#1a7f37",
		AddedBg:   "#dafbe1",
		RemovedFg: "#cf222e",
		RemovedBg: "#ffebe9",
		HunkFg:    "#0969da",

		LineNumFg: "#8c959f",

		SelectedBg: "#f6f8fa",
		SelectedFg: "#1f2328",
		StagedFg:   "#1a7f37",
		ModifiedFg: "#9a6700",
		AddedFileFg:  "#1a7f37",
		DeletedFg:  "#cf222e",
		RenamedFg:  "#8250df",
		UntrackedFg: "#656d76",

		BorderFg:    "#d0d7de",
		StatusBarBg: "#f6f8fa",
		StatusBarFg: "#656d76",
		HelpKeyFg:   "#0969da",
		HelpDescFg:  "#656d76",

		AccentFg: "#0969da",

		ChromaStyle: "github",
	}
}
