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

	// AddedEmphBg and RemovedEmphBg paint the part of a line that actually
	// differs from the line it is paired with in split view. They sit a step
	// away from AddedBg/RemovedBg — far enough to see, close enough that the
	// span still reads as part of the same line rather than a different kind
	// of line.
	AddedEmphBg   string
	RemovedEmphBg string

	// MarkFg draws the marks differ adds inside the code column: the stand-in
	// for trailing whitespace, and the sign that a line was cut to fit. It is
	// deliberately neutral — these are notes about the line, not part of the
	// add/remove language — and has to be legible against both diff
	// backgrounds.
	MarkFg string

	// Line numbers
	LineNumFg        string
	LineNumAddedFg   string
	LineNumRemovedFg string

	// Header bar
	HeaderBg string
	HeaderFg string

	// Hunk
	HunkBg string

	// File list
	SelectedBg  string
	SelectedFg  string
	StagedFg    string
	ModifiedFg  string
	AddedFileFg string
	DeletedFg   string
	RenamedFg   string
	UntrackedFg string

	// Card
	CardBg string

	// Chrome — deliberately dim. Colour is reserved for content that carries
	// meaning: diff lines, file status, review state, syntax.
	ChromeFg       string
	PanelLabelFg   string
	HeaderNameFg   string
	HeaderBranchFg string
	HeaderMetaFg   string

	// Chrome
	BorderFg    string
	StatusBarBg string
	StatusBarFg string
	HelpKeyFg   string
	HelpDescFg  string

	// Review comments
	CommentFg     string
	CommentMetaFg string
	StaleFg       string

	// Semantic colours, named for what they mean rather than where they are
	// used, so a new piece of UI does not need a new field.
	SuccessFg string
	WarningFg string
	ErrorFg   string
	MutedFg   string

	// Accent
	AccentFg string

	// Chroma syntax theme name
	ChromaStyle string
}

// Themes is the registry of built-in themes.
//
// Each is paired with the Chroma style of the same family, so the chrome and
// the syntax highlighting agree. "dark" and "light" are the names differ
// shipped with and stay as aliases, so nobody's config breaks.
//
// Gruvbox, Tokyo Night and GitHub Dark are taken value-for-value from their
// upstream palettes, with the token names in comments so they can be checked
// again later. Mocha and Latte are *derived* from Catppuccin rather than equal
// to it — they are differ's original two themes, and a fair number of their
// values (the purple, the staged green, the surfaces) come from elsewhere or
// from nowhere. Renaming them did not change them, and this comment says so
// rather than claiming a provenance they do not have.
var Themes = map[string]Theme{
	"mocha":      MochaTheme(),
	"latte":      LatteTheme(),
	"gruvbox":    GruvboxTheme(),
	"tokyonight": TokyoNightTheme(),
	"github":     GitHubDarkTheme(),

	"dark":  MochaTheme(),
	"light": LatteTheme(),
}

// ThemeNames lists the registry in a stable order, for error messages and the
// picker.
func ThemeNames() []string {
	return []string{"mocha", "latte", "gruvbox", "tokyonight", "github"}
}

// DarkTheme is the original name for Mocha, kept because cmd/ and the tests
// have always called it.
func DarkTheme() Theme { return MochaTheme() }

// LightTheme is the original name for Latte.
func LightTheme() Theme { return LatteTheme() }

// MochaTheme is differ's original dark theme, derived from Catppuccin Mocha.
// https://github.com/catppuccin/catppuccin — MIT. Not value-for-value: see
// the note on Themes.
func MochaTheme() Theme {
	return Theme{
		Bg: "#1e1e2e",
		Fg: "#e0e0f0",

		AddedFg:   "#a6e3a1",
		AddedBg:   "#1e3a2c",
		RemovedFg: "#f38ba8",
		RemovedBg: "#3b1d2e",
		HunkFg:    "#6c5ce7",

		AddedEmphBg:   "#2f5d43",
		RemovedEmphBg: "#5e2b3d",

		MarkFg: "#9399b2",

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

		ChromeFg:       "#45475a",
		PanelLabelFg:   "#6c7086",
		HeaderNameFg:   "#cba6f7",
		HeaderBranchFg: "#a6adc8",
		HeaderMetaFg:   "#6c7086",

		BorderFg:    "#6c5ce7",
		StatusBarBg: "#1a1a2e",
		StatusBarFg: "#b4befe",
		HelpKeyFg:   "#c678dd",
		HelpDescFg:  "#9399b2",

		CommentFg:     "#f9e2af",
		CommentMetaFg: "#9399b2",
		StaleFg:       "#fab387",

		SuccessFg: "#a6e3a1",
		WarningFg: "#f9e2af",
		ErrorFg:   "#f38ba8",
		MutedFg:   "#9399b2",

		AccentFg: "#c678dd",

		ChromaStyle: "catppuccin-mocha",
	}
}

// LatteTheme is differ's original light theme, derived from Catppuccin Latte.
// https://github.com/catppuccin/catppuccin — MIT. Not value-for-value: see
// the note on Themes.
func LatteTheme() Theme {
	return Theme{
		Bg: "#eff1f5",
		Fg: "#4c4f69",

		AddedFg:   "#1a7f2a",
		AddedBg:   "#e6f5e4",
		RemovedFg: "#d20f39",
		RemovedBg: "#fde4e8",
		HunkFg:    "#1e66f5",

		AddedEmphBg:   "#c3e8bd",
		RemovedEmphBg: "#f7c6d0",

		MarkFg: "#6c6f85",

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

		ChromeFg:       "#bcc0cc",
		PanelLabelFg:   "#8c8fa1",
		HeaderNameFg:   "#8839ef",
		HeaderBranchFg: "#5c5f77",
		HeaderMetaFg:   "#8c8fa1",

		BorderFg:    "#8839ef",
		StatusBarBg: "#e6e9ef",
		StatusBarFg: "#6c6f85",
		HelpKeyFg:   "#8839ef",
		HelpDescFg:  "#8c8fa1",

		// Catppuccin Latte's own yellow (#df8e1d), green (#40a02b) and peach
		// (#fe640b) are 2.3, 3.0 and 2.6 against its base — fine as an accent,
		// unreadable as the text of a review comment. These are the same hues
		// darkened until they clear the bar. The palette has no legible
		// alternative to darken towards, which is why they are derived rather
		// than picked from it.
		CommentFg:     "#8a5a00",
		CommentMetaFg: "#8c8fa1",
		StaleFg:       "#a64100",

		SuccessFg: "#2d7a1f",
		WarningFg: "#8a5a00",
		ErrorFg:   "#d20f39",
		MutedFg:   "#6c6f85",

		AccentFg: "#8839ef",

		ChromaStyle: "catppuccin-latte",
	}
}

// NoColorTheme is the theme for --no-color and NO_COLOR: every colour is
// empty, which lipgloss renders as no colour at all, and syntax highlighting
// is off.
//
// It is the zero Theme by design. A colour added to Theme later is then
// colourless here automatically, rather than quietly reappearing in a mode
// that promised none.
func NoColorTheme() Theme {
	return Theme{ChromaStyle: NoHighlight}
}

// NoHighlight turns syntax highlighting off. An empty style name cannot: it
// falls back to a default, which would put the colour straight back.
const NoHighlight = "none"

// GruvboxTheme is Gruvbox Dark, in its "medium" contrast.
// https://github.com/morhetz/gruvbox — MIT.
func GruvboxTheme() Theme {
	return Theme{
		Bg: "#282828", // bg0
		Fg: "#ebdbb2", // fg1

		AddedFg:   "#b8bb26", // bright green
		AddedBg:   "#32361a",
		RemovedFg: "#fb4934", // bright red
		RemovedBg: "#3c1f1e",
		HunkFg:    "#83a598", // bright blue

		AddedEmphBg:   "#4a4a1e",
		RemovedEmphBg: "#5a2f2a",

		MarkFg: "#a89984", // fg4

		LineNumFg:        "#7c6f64", // bg4
		LineNumAddedFg:   "#b8bb26",
		LineNumRemovedFg: "#fb4934",

		HeaderBg: "#3c3836", // bg1
		HeaderFg: "#d3869b", // bright purple

		HunkBg: "#3c3836",
		CardBg: "#32302f", // bg0_s

		SelectedBg:  "#504945", // bg2
		SelectedFg:  "#fbf1c7", // fg0
		StagedFg:    "#b8bb26",
		ModifiedFg:  "#fabd2f", // bright yellow
		AddedFileFg: "#b8bb26",
		DeletedFg:   "#fb4934",
		RenamedFg:   "#d3869b",
		UntrackedFg: "#928374", // gray

		ChromeFg:       "#504945",
		PanelLabelFg:   "#928374",
		HeaderNameFg:   "#d3869b",
		HeaderBranchFg: "#bdae93", // fg3
		HeaderMetaFg:   "#928374",

		BorderFg:    "#665c54", // bg3
		StatusBarBg: "#1d2021", // bg0_h
		StatusBarFg: "#bdae93",
		HelpKeyFg:   "#8ec07c", // bright aqua
		HelpDescFg:  "#a89984",

		CommentFg:     "#fabd2f",
		CommentMetaFg: "#a89984",
		StaleFg:       "#fe8019", // bright orange

		SuccessFg: "#b8bb26",
		WarningFg: "#fabd2f",
		ErrorFg:   "#fb4934",
		MutedFg:   "#a89984",

		AccentFg: "#d3869b",

		ChromaStyle: "gruvbox",
	}
}

// TokyoNightTheme is Tokyo Night, the "night" variant.
// https://github.com/folke/tokyonight.nvim — Apache-2.0.
func TokyoNightTheme() Theme {
	return Theme{
		Bg: "#1a1b26",
		Fg: "#c0caf5",

		AddedFg:   "#9ece6a",
		AddedBg:   "#20303b",
		RemovedFg: "#f7768e",
		RemovedBg: "#37222c",
		HunkFg:    "#7aa2f7",

		AddedEmphBg:   "#2c4a3e",
		RemovedEmphBg: "#4c2b38",

		// Not an upstream token: the palette's comment colour (#565f89) is
		// 2.2 against the diff backgrounds, and these marks are notes about
		// the code rather than chrome, so they have to be legible.
		MarkFg: "#8189ad",

		LineNumFg:        "#3b4261",
		LineNumAddedFg:   "#9ece6a",
		LineNumRemovedFg: "#f7768e",

		// bg_highlight and bg_dark are the night variant's own; #24283b and
		// #1f2335 are storm's, which is what these were.
		HeaderBg: "#292e42",
		HeaderFg: "#bb9af7",

		HunkBg: "#292e42",
		CardBg: "#16161e",

		SelectedBg:  "#33467c",
		SelectedFg:  "#c0caf5",
		StagedFg:    "#9ece6a",
		ModifiedFg:  "#e0af68",
		AddedFileFg: "#9ece6a",
		DeletedFg:   "#f7768e",
		RenamedFg:   "#bb9af7",
		UntrackedFg: "#565f89",

		ChromeFg:       "#3b4261",
		PanelLabelFg:   "#565f89",
		HeaderNameFg:   "#bb9af7",
		HeaderBranchFg: "#a9b1d6",
		HeaderMetaFg:   "#565f89",

		BorderFg:    "#3b4261",
		StatusBarBg: "#16161e",
		StatusBarFg: "#a9b1d6",
		HelpKeyFg:   "#7dcfff",
		HelpDescFg:  "#565f89",

		CommentFg:     "#e0af68",
		CommentMetaFg: "#565f89",
		StaleFg:       "#ff9e64",

		SuccessFg: "#9ece6a",
		WarningFg: "#e0af68",
		ErrorFg:   "#f7768e",
		MutedFg:   "#a9b1d6", // fg_dark — muted, but still text

		AccentFg: "#bb9af7",

		ChromaStyle: "tokyonight-night",
	}
}

// GitHubDarkTheme is GitHub's dark default.
// https://primer.style — MIT.
func GitHubDarkTheme() Theme {
	return Theme{
		Bg: "#0d1117",
		Fg: "#c9d1d9",

		AddedFg:   "#3fb950",
		AddedBg:   "#12261e",
		RemovedFg: "#ff7b72",
		RemovedBg: "#25171c",
		HunkFg:    "#58a6ff",

		AddedEmphBg:   "#1b4721",
		RemovedEmphBg: "#542426",

		MarkFg: "#8b949e",

		LineNumFg:        "#484f58",
		LineNumAddedFg:   "#3fb950",
		LineNumRemovedFg: "#ff7b72",

		HeaderBg: "#161b22",
		HeaderFg: "#bc8cff",

		HunkBg: "#161b22",
		CardBg: "#161b22",

		SelectedBg:  "#1f6feb",
		SelectedFg:  "#f0f6fc",
		StagedFg:    "#3fb950",
		ModifiedFg:  "#d29922",
		AddedFileFg: "#3fb950",
		DeletedFg:   "#ff7b72",
		RenamedFg:   "#bc8cff",
		UntrackedFg: "#6e7681",

		ChromeFg:       "#30363d",
		PanelLabelFg:   "#8b949e",
		HeaderNameFg:   "#bc8cff",
		HeaderBranchFg: "#c9d1d9",
		HeaderMetaFg:   "#8b949e",

		BorderFg:    "#30363d",
		StatusBarBg: "#010409",
		StatusBarFg: "#c9d1d9",
		HelpKeyFg:   "#58a6ff",
		HelpDescFg:  "#8b949e",

		CommentFg:     "#d29922",
		CommentMetaFg: "#8b949e",
		StaleFg:       "#db6d28",

		SuccessFg: "#3fb950",
		WarningFg: "#d29922",
		ErrorFg:   "#ff7b72",
		MutedFg:   "#8b949e",

		AccentFg: "#bc8cff",

		ChromaStyle: "github-dark",
	}
}
