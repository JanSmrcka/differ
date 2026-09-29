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

// A theme's Chroma style has to exist. Naming one Chroma does not have falls
// back to monokai, which looks nothing like the rest of the theme.
func TestThemes_EveryChromaStyleExists(t *testing.T) {
	t.Parallel()
	for name, th := range Themes {
		if styles.Get(th.ChromaStyle) == nil {
			t.Errorf("%s names Chroma style %q, which does not exist", name, th.ChromaStyle)
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
func relativeLuminance(hex string) float64 {
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
	}
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
