package theme

import (
	"math"
	"reflect"
	"regexp"
	"strconv"
	"testing"

	"github.com/alecthomas/chroma/v2/styles"
)

func TestThemes_MapCompleteness(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"mocha", "latte", "gruvbox", "tokyonight", "github",
		// The original two names, kept so nobody's config breaks.
		"dark", "light",
	} {
		if _, ok := Themes[name]; !ok {
			t.Errorf("Themes map missing %q", name)
		}
	}
}

// Every theme in the registry is held to the same standard. The checks used to
// be written out per theme, which meant a theme added later was covered by
// nothing until someone remembered to add four more test functions.
func TestThemes_EveryThemeIsComplete(t *testing.T) {
	t.Parallel()
	for name, th := range Themes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkNonEmpty(t, th, name)
			checkValidHex(t, th, name)
			checkContrast(t, th, name)
			if th.ChromaStyle == "" {
				t.Errorf("%s has no Chroma style, so its diffs would be unhighlighted", name)
			}
		})
	}
}

// A theme's Chroma style has to exist.
//
// The check is a registry lookup, not styles.Get: Get returns Chroma's
// Fallback — a near-monochrome style called "swapoff" — for any name it does
// not know, and never nil. Written the obvious way this test could not fail,
// and a typo'd style name would have shipped as a colourless diff.
func TestThemes_EveryChromaStyleExists(t *testing.T) {
	t.Parallel()
	for name, th := range Themes {
		if _, ok := styles.Registry[th.ChromaStyle]; !ok {
			t.Errorf("%s names Chroma style %q, which does not exist", name, th.ChromaStyle)
		}
	}
}

// ThemeNames is what the error message and --help offer, so it has to be the
// registry minus the aliases — nothing more, nothing less. Three mistakes were
// possible here and none of them failed anything: a theme in the map but not
// the list (usable but unlisted), a name in the list but not the map (offered,
// then refused), and the aliases leaking into the list.
func TestThemes_ThemeNamesMatchesTheRegistry(t *testing.T) {
	t.Parallel()
	aliases := map[string]bool{"dark": true, "light": true}

	listed := map[string]bool{}
	for _, name := range ThemeNames() {
		if _, ok := Themes[name]; !ok {
			t.Errorf("ThemeNames offers %q, which is not in the registry", name)
		}
		if aliases[name] {
			t.Errorf("ThemeNames offers the alias %q; it should list the real names", name)
		}
		listed[name] = true
	}
	for name := range Themes {
		if !aliases[name] && !listed[name] {
			t.Errorf("%q is in the registry but ThemeNames does not offer it", name)
		}
	}
}

// The aliases have to be the same themes, not copies that can drift.
func TestThemes_AliasesPointAtTheSameTheme(t *testing.T) {
	t.Parallel()
	for alias, real := range map[string]string{"dark": "mocha", "light": "latte"} {
		if !reflect.DeepEqual(Themes[alias], Themes[real]) {
			t.Errorf("%q and %q have drifted apart", alias, real)
		}
	}
}

func checkNonEmpty(t *testing.T, th Theme, label string) {
	t.Helper()
	v := reflect.ValueOf(th)
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		name := typ.Field(i).Name
		if field.Kind() == reflect.String && field.String() == "" {
			t.Errorf("%s.%s is empty", label, name)
		}
	}
}

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func checkValidHex(t *testing.T, th Theme, label string) {
	t.Helper()
	v := reflect.ValueOf(th)
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		name := typ.Field(i).Name
		if field.Kind() != reflect.String || name == "ChromaStyle" {
			continue
		}
		if !hexColorRe.MatchString(field.String()) {
			t.Errorf("%s.%s = %q is not valid #RRGGBB", label, name, field.String())
		}
	}
}

// relativeLuminance computes WCAG relative luminance from a hex color.
//
// A malformed value returns 0 rather than panicking. It used to slice blindly,
// so one short hex took the whole test binary down and every other parallel
// subtest's result with it — checkValidHex had already reported the real
// problem a moment earlier.
func relativeLuminance(hex string) float64 {
	if !hexColorRe.MatchString(hex) {
		return 0
	}
	r, _ := strconv.ParseInt(hex[1:3], 16, 64)
	g, _ := strconv.ParseInt(hex[3:5], 16, 64)
	b, _ := strconv.ParseInt(hex[5:7], 16, 64)
	linearize := func(c int64) float64 {
		s := float64(c) / 255.0
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*linearize(r) + 0.7152*linearize(g) + 0.0722*linearize(b)
}

// contrastRatio computes WCAG contrast ratio between two hex colors.
func contrastRatio(hex1, hex2 string) float64 {
	l1 := relativeLuminance(hex1)
	l2 := relativeLuminance(hex2)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

type contrastPair struct {
	fg, bg   string
	minRatio float64
	label    string
}

func checkContrast(t *testing.T, th Theme, label string) {
	t.Helper()
	pairs := []contrastPair{
		{th.Fg, th.Bg, 4.5, "Fg/Bg"},
		{th.AddedFg, th.AddedBg, 3.0, "AddedFg/AddedBg"},
		{th.RemovedFg, th.RemovedBg, 3.0, "RemovedFg/RemovedBg"},
		{th.HeaderFg, th.HeaderBg, 3.0, "HeaderFg/HeaderBg"},
		{th.SelectedFg, th.SelectedBg, 3.0, "SelectedFg/SelectedBg"},
		{th.StatusBarFg, th.StatusBarBg, 3.0, "StatusBarFg/StatusBarBg"},
		{th.Fg, th.CardBg, 4.5, "Fg/CardBg"},
		{th.HelpKeyFg, th.Bg, 3.0, "HelpKeyFg/Bg"},
		// The marks inside the code column sit on a diff background, not on
		// the page background.
		{th.MarkFg, th.AddedBg, 3.0, "MarkFg/AddedBg"},
		{th.MarkFg, th.RemovedBg, 3.0, "MarkFg/RemovedBg"},
		// The emphasised span still has to be readable, not just visible.
		{th.AddedFg, th.AddedEmphBg, 3.0, "AddedFg/AddedEmphBg"},
		{th.RemovedFg, th.RemovedEmphBg, 3.0, "RemovedFg/RemovedEmphBg"},
		// A review comment body is prose the user wrote, and it is rendered as
		// its own rows with no background of its own — so it sits on the page
		// background and gets the body-text bar. Its marker glyph in the
		// gutter is the part that sits on a diff background, and a glyph is
		// not text.
		{th.CommentFg, th.Bg, 4.5, "CommentFg/Bg (body)"},
		{th.CommentFg, th.AddedBg, 3.0, "CommentFg/AddedBg (gutter mark)"},
		{th.CommentFg, th.RemovedBg, 3.0, "CommentFg/RemovedBg (gutter mark)"},
		// The labels: the comment's "line 12 · pending" row, the command bar,
		// and the header's summary. Not prose, but read rather than glanced
		// at, so a 3.0 floor rather than 4.5.
		{th.CommentMetaFg, th.Bg, 3.0, "CommentMetaFg/Bg"},
		{th.HelpDescFg, th.Bg, 3.0, "HelpDescFg/Bg"},
		{th.HeaderMetaFg, th.Bg, 3.0, "HeaderMetaFg/Bg"},
		// The colours that say what happened.
		{th.SuccessFg, th.Bg, 3.0, "SuccessFg/Bg"},
		{th.WarningFg, th.Bg, 3.0, "WarningFg/Bg"},
		{th.ErrorFg, th.Bg, 3.0, "ErrorFg/Bg"},
		{th.MutedFg, th.Bg, 3.0, "MutedFg/Bg"},
		{th.StaleFg, th.Bg, 3.0, "StaleFg/Bg"},
		// The rest of what is actually painted on the page background.
		{th.HeaderNameFg, th.Bg, 3.0, "HeaderNameFg/Bg"},
		{th.HeaderBranchFg, th.Bg, 3.0, "HeaderBranchFg/Bg"},
		{th.AccentFg, th.Bg, 3.0, "AccentFg/Bg"},
		{th.HunkFg, th.Bg, 3.0, "HunkFg/Bg"},
		// The selected row sets a foreground only — SelectedBg has no painter
		// — so what matters is the page background behind it.
		{th.SelectedFg, th.Bg, 3.0, "SelectedFg/Bg"},
	}
	// Deliberately absent, and this is the whole list: ChromeFg, BorderFg,
	// PanelLabelFg, LineNumFg and UntrackedFg. Those are dim on purpose — the
	// struct says as much — and they sit between 1.7 and 2.8 in every theme,
	// including the two differ shipped with. A 3.0 bar there would not make
	// them legible; it would stop them being chrome.
	//
	// SelectedBg, CardBg and StatusBarBg are gated above against their own
	// foregrounds, but nothing paints them today: FileSelected, the status row
	// and the header all set a foreground only, and renderCard is gone. The
	// pairs are kept so the fields stay coherent if anything ever does.
	for _, p := range pairs {
		ratio := contrastRatio(p.fg, p.bg)
		if ratio < p.minRatio {
			t.Errorf("%s %s: contrast %.2f < %.1f (fg=%s bg=%s)",
				label, p.label, ratio, p.minRatio, p.fg, p.bg)
		}
	}
}

// TestContrastRatio_KnownValues verifies the formula against known WCAG values.
func TestContrastRatio_KnownValues(t *testing.T) {
	t.Parallel()
	// Black on white = 21:1
	ratio := contrastRatio("#ffffff", "#000000")
	if math.Abs(ratio-21.0) > 0.1 {
		t.Errorf("white/black contrast = %.2f, want ~21.0", ratio)
	}
	// Same color = 1:1
	ratio = contrastRatio("#888888", "#888888")
	if math.Abs(ratio-1.0) > 0.01 {
		t.Errorf("same color contrast = %.2f, want 1.0", ratio)
	}
}

func TestThemes_DarkEqualsFunction(t *testing.T) {
	t.Parallel()
	if !reflect.DeepEqual(Themes["dark"], DarkTheme()) {
		t.Error("Themes[dark] != DarkTheme()")
	}
}

func TestThemes_LightEqualsFunction(t *testing.T) {
	t.Parallel()
	if !reflect.DeepEqual(Themes["light"], LightTheme()) {
		t.Error("Themes[light] != LightTheme()")
	}
}

// The gate itself has to bite. Without this it could be emptied, short-circuited
// or left to drift into a no-op and every theme would still "pass".
func TestCheckContrast_Bites(t *testing.T) {
	t.Parallel()
	// A theme where everything is the background: every pair is 1.00.
	flat := Theme{ChromaStyle: "monokai"}
	v := reflect.ValueOf(&flat).Elem()
	for i := 0; i < v.NumField(); i++ {
		if f := v.Field(i); f.Kind() == reflect.String && v.Type().Field(i).Name != "ChromaStyle" {
			f.SetString("#808080")
		}
	}

	fake := &testing.T{}
	checkContrast(fake, flat, "flat")
	if !fake.Failed() {
		t.Error("checkContrast passed a theme whose every colour is identical")
	}
}

// And a malformed hex must not take the run down with it.
func TestRelativeLuminance_SurvivesAMalformedHex(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "#", "#1e2", "not a colour", "#12345g"} {
		if got := relativeLuminance(bad); got != 0 {
			t.Errorf("relativeLuminance(%q) = %v, want 0", bad, got)
		}
		_ = contrastRatio(bad, "#ffffff")
	}
}
