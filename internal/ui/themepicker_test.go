package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/testutil"
	"github.com/jansmrcka/differ/internal/theme"
)

// Trying a theme meant editing a config file or restarting with --theme, which
// is a poor way to find out whether you like one.

func pickerModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.ts", Status: git.StatusModified}}})
	m.theme = theme.Themes["mocha"]
	m.styles = NewStyles(m.theme)
	return m
}

func TestThemePicker_OpensListingEveryThemeWithTheCurrentOneMarked(t *testing.T) {
	t.Parallel()
	// The picker opens on the theme actually in effect, which is not
	// necessarily the one in the config — --theme overrides it.
	m := pickerModel(t)
	m.cfg.Theme = "gruvbox"
	m.theme = theme.Themes["gruvbox"]
	m.styles = NewStyles(m.theme)

	updated, _ := m.routeKey(key("t"))
	m = updated.(Model)
	if !m.showThemes {
		t.Fatal("t did not open the picker")
	}

	got := stripANSI(m.renderThemeOverlay(80, 16))
	for _, name := range theme.ThemeNames() {
		if !strings.Contains(got, name) {
			t.Errorf("the picker does not offer %q:\n%s", name, got)
		}
	}
	// It opens on whatever is in use, not at the top of the list.
	if theme.ThemeNames()[m.themeCursor] != "gruvbox" {
		t.Errorf("the cursor starts on %q, want the theme in use",
			theme.ThemeNames()[m.themeCursor])
	}
}

// Moving the selection changes the colours there and then — that is the point
// of a picker over editing a config file.
func TestThemePicker_MovingTheSelectionPreviewsIt(t *testing.T) {
	t.Parallel()
	m := pickerModel(t)
	updated, _ := m.routeKey(key("t"))
	m = updated.(Model)
	before := m.theme

	moved, cmd := m.routeKey(key("j"))
	m = moved.(Model)
	if m.theme == before {
		t.Error("moving the selection did not change the theme")
	}
	if m.theme != theme.Themes[theme.ThemeNames()[m.themeCursor]] {
		t.Error("the preview does not match the selection")
	}
	// Not m.styles.DiffAdded.String(): that is Render(""), which is empty for
	// every style under the test profile. What is observable is that a
	// re-render was scheduled — DiffRenderer bakes both the styles and the
	// syntax palette into each cached row, so recolouring is not enough — and
	// that the renderer built from this theme picks up its palette.
	if cmd == nil {
		t.Error("no re-render was scheduled for the previewed theme")
	}
	if got := chromaStyleFor(m.theme.ChromaStyle); got != chromaStyleFor(theme.Themes["latte"].ChromaStyle) {
		t.Error("the previewed theme does not resolve to its own syntax palette")
	}
}

// Cancelling puts back exactly what was there and writes nothing.
func TestThemePicker_EscRestoresTheThemeAndSavesNothing(t *testing.T) {
	t.Parallel()
	// liveModel, not pickerModel: this test runs the command it gets back, and
	// cancelling schedules a re-render that talks to git.
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "init")
	tr.Modify("a.ts", "two\n")
	m := liveModel(t, tr)
	m.theme = theme.Themes["mocha"]
	m.styles = NewStyles(m.theme)
	before := m.theme

	updated, _ := m.routeKey(key("t"))
	m = updated.(Model)
	for range 2 {
		moved, _ := m.routeKey(key("j"))
		m = moved.(Model)
	}
	if m.theme == before {
		t.Fatal("the preview never changed")
	}

	closed, cmd := m.routeKey(tea.KeyMsg{Type: tea.KeyEsc})
	got := closed.(Model)
	if got.showThemes {
		t.Error("esc did not close the picker")
	}
	if got.theme != before {
		t.Error("esc did not put the original theme back")
	}
	if got.cfg.Theme == theme.ThemeNames()[got.themeCursor] && got.cfg.Theme != "" {
		t.Error("esc recorded the previewed theme in the config")
	}
	// A re-render is expected — the original colours have to go back on the
	// screen. A config write is not.
	if cmd != nil {
		if _, saved := cmd().(savePrefDoneMsg); saved {
			t.Error("esc saved the config; cancelling must write nothing")
		}
	}
}

// Confirming keeps the theme and remembers it for next time.
func TestThemePicker_EnterKeepsTheThemeAndPersistsIt(t *testing.T) {
	t.Parallel()
	m := pickerModel(t)
	updated, _ := m.routeKey(key("t"))
	m = updated.(Model)
	moved, _ := m.routeKey(key("j"))
	m = moved.(Model)

	chosen := theme.ThemeNames()[m.themeCursor]
	confirmed, cmd := m.routeKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := confirmed.(Model)

	if got.showThemes {
		t.Error("enter did not close the picker")
	}
	if got.theme != theme.Themes[chosen] {
		t.Error("enter did not keep the previewed theme")
	}
	if got.cfg.Theme != chosen {
		t.Errorf("cfg.Theme = %q, want %q", got.cfg.Theme, chosen)
	}
	if cmd == nil {
		t.Error("enter did not save the choice")
	}
}

// The syntax highlighting has to follow the theme, which it could not: the
// Chroma style was one package-level variable, first behind a sync.Once — so
// the session's first theme was the only one that took effect — and then
// behind a mutex, which fixed the memory safety and not the fact that two
// renderers can be in flight at different themes. It is resolved per theme
// now, and a renderer carries its own.
func TestThemePicker_EachThemeResolvesItsOwnSyntaxPalette(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, name := range theme.ThemeNames() {
		style := chromaStyleFor(theme.Themes[name].ChromaStyle)
		if style == nil {
			t.Errorf("%s resolved to no palette at all", name)
			continue
		}
		if seen[style.Name] {
			t.Errorf("%s shares a palette with an earlier theme (%s)", name, style.Name)
		}
		seen[style.Name] = true
	}

	// And a name Chroma does not have falls back to something legible rather
	// than to its near-monochrome Fallback.
	if got := chromaStyleFor("definitely-not-a-style"); got == nil || got.Name == "swapoff" {
		t.Errorf("an unknown style resolved to %v, want the monokai fallback", got)
	}
	// No colour means no palette.
	if got := chromaStyleFor(theme.NoHighlight); got != nil {
		t.Errorf("NoHighlight resolved to %v, want none", got)
	}
}

// Writing the whole config means every other preference has to be current in
// it. The split toggle kept its answer in the model only, so choosing a theme
// serialised the value the session started with and quietly undid the toggle.
func TestThemePicker_KeepingAThemeDoesNotUndoTheSplitToggle(t *testing.T) {
	t.Parallel()
	m := pickerModel(t)
	m.cfg.SplitDiff = false
	m.splitDiff = false

	toggled, _ := m.updateDiffMode(key("v"))
	m = toggled.(Model)
	if !m.splitDiff {
		t.Fatal("v did not turn split on")
	}
	if !m.cfg.SplitDiff {
		t.Error("the toggle did not reach the config the theme write serialises")
	}

	opened, _ := m.routeKey(key("t"))
	m = opened.(Model)
	confirmed, _ := m.routeKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := confirmed.(Model).cfg.SplitDiff; !got {
		t.Error("keeping a theme wrote split_diff back to what the session started with")
	}
}

// The picker is on screen and the user is choosing a theme. A key it does not
// use must not reach the view underneath and do something to the repository.
func TestThemePicker_SwallowsKeysThatWouldActOnTheRepo(t *testing.T) {
	t.Parallel()
	m := pickerModel(t)
	m.mode = modeFileList
	opened, _ := m.routeKey(key("t"))
	m = opened.(Model)

	// tab is built by hand: key() only makes single runes and quietly returns
	// a down arrow for anything longer, which the picker *does* handle — so
	// the test would have been exercising the cursor, not the swallow.
	keys := []tea.KeyMsg{{Type: tea.KeyTab}}
	for _, r := range []string{"a", "v", "c", "r", "b", "P", "F"} {
		keys = append(keys, key(r))
	}

	for _, k := range keys {
		updated, cmd := m.routeKey(k)
		got := updated.(Model)
		if cmd != nil {
			t.Errorf("%q acted while the picker was open", k.String())
		}
		if got.mode != modeFileList || !got.showThemes {
			t.Errorf("%q left the picker: mode=%v showThemes=%v", k.String(), got.mode, got.showThemes)
		}
	}
}

// "in use" marks the theme actually in effect. It was compared by name against
// the config, which says "dark" on a default install — an alias that is not in
// the list — so nothing was marked at all.
func TestThemePicker_MarksTheThemeInUseEvenUnderAnAlias(t *testing.T) {
	t.Parallel()
	m := pickerModel(t)
	m.cfg.Theme = "dark" // the alias a default config carries
	m.theme = theme.Themes["dark"]
	m.styles = NewStyles(m.theme)

	opened, _ := m.openThemePicker()
	got := stripANSI(opened.renderThemeOverlay(60, 16))
	if !strings.Contains(got, "in use") {
		t.Errorf("nothing is marked as in use:\n%s", got)
	}
	// And on the right row: dark is Mocha.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "in use") && !strings.Contains(line, "mocha") {
			t.Errorf("the wrong row is marked in use: %q", line)
		}
	}
}

// With colour off there is no theme to go back to, so keeping one has to mean
// keeping it — enter used to write a name while the session stayed colourless.
func TestThemePicker_EnterAppliesWhatItWrites(t *testing.T) {
	t.Parallel()
	m := pickerModel(t)
	plain := theme.NoColorTheme()
	m.theme, m.styles = plain, NewStyles(plain)

	opened, _ := m.openThemePicker()
	confirmed, _ := opened.confirmTheme()

	name := confirmed.cfg.Theme
	if confirmed.theme != theme.Themes[name] {
		t.Errorf("wrote theme %q but the session is using something else", name)
	}
}
