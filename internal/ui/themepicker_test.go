package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/config"
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

// A preview that was never confirmed must not survive the picker being closed
// from underneath it.
//
// The branch list arrives asynchronously, and handleBranchesLoaded closes every
// overlay because a different view is taking over. It was clearing showThemes
// directly, which dropped the picker without putting the theme back: press b,
// press t, move the cursor, and the list lands — the session then runs in a
// theme the user never chose, nothing is written to config, and esc can no
// longer undo it.
func TestThemePicker_TheBranchListArrivingRestoresThePreviewedTheme(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("a.ts", "one\n", "first")
	tr.Modify("a.ts", "two\n")
	m := liveModel(t, tr)

	before, beforeCfg := m.theme, m.cfg.Theme
	m, _ = m.openThemePicker()
	m, _ = m.moveThemeCursor(1)
	if m.theme == before {
		t.Fatal("moving the cursor did not preview a different theme")
	}

	updated, _ := m.Update(branchesLoadedMsg{branches: []string{"master", "other"}, current: "master"})
	m = updated.(Model)

	if m.showThemes {
		t.Error("the picker is still open after the branch list arrived")
	}
	if m.theme != before {
		t.Error("an unconfirmed preview survived the picker being closed")
	}
	if m.cfg.Theme != beforeCfg {
		t.Errorf("nothing was confirmed, but the config moved to theme=%q", m.cfg.Theme)
	}
}

// The picker has to be exercised through View(), not only through
// renderThemeOverlay. Three separate sabotages of layout.go left the suite
// green — widening the overlay to the whole terminal, moving it into the
// overlay switch so the diff is hidden, and disabling it entirely — which
// means the change this PR exists to make was defended by nothing.
func TestThemePicker_TheDiffStaysOnScreenBesideThePicker(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\ntwo\n", "first")
	tr.Modify("src.ts", "one\nCHANGED\n")
	m := liveModel(t, tr)

	closed := m.View()
	if !strings.Contains(closed, "CHANGED") {
		t.Fatal("the diff is not on screen before the picker opens")
	}

	m, _ = m.openThemePicker()
	open := m.View()

	if !strings.Contains(open, "theme") {
		t.Error("the picker is not drawn at all")
	}
	// The whole point: the theme under the cursor is judged by the diff, so
	// the diff has to still be there.
	if !strings.Contains(open, "CHANGED") {
		t.Errorf("the picker hid the diff:\n%s", open)
	}
	// And it takes the file list's panel, so the divider survives.
	if !strings.Contains(open, verticalDivider) {
		t.Errorf("the picker took the whole width:\n%s", open)
	}
	for i, row := range strings.Split(open, "\n") {
		if w := lipgloss.Width(row); w > m.width {
			t.Errorf("row %d is %d wide in a %d-column terminal", i, w, m.width)
		}
	}
}

// A diff that was built under the old palette must be dropped, not installed.
// Both halves of the mechanism were unprotected: deleting the guard was green,
// and so was stamping every load with 0 — which would freeze the diff pane for
// the rest of the session after the first theme change.
func TestThemePicker_ADiffBuiltUnderTheOldPaletteIsDropped(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")
	m := liveModel(t, tr)

	// A load issued now carries the current generation and is installed.
	fresh, ok := m.loadDiffCmd(false)().(diffLoadedMsg)
	if !ok {
		t.Fatal("loadDiffCmd did not produce a diffLoadedMsg")
	}
	if fresh.themeGen != m.themeGen {
		t.Fatalf("a fresh load is stamped %d, but the model is at %d", fresh.themeGen, m.themeGen)
	}

	// Switching theme invalidates it.
	m, _ = m.applyTheme(theme.Themes["gruvbox"])
	if fresh.themeGen == m.themeGen {
		t.Fatal("applyTheme did not advance the generation")
	}

	before := m.renderer
	updated, _ := m.Update(fresh)
	m = updated.(Model)
	if m.renderer != before {
		t.Error("a diff built under the previous palette was installed anyway")
	}

	// And the other half: a load issued *after* the switch has to be installed.
	// Stamping every load with a constant passes the check above and freezes
	// the diff pane for the rest of the session — the guard then rejects
	// everything, because the model's generation has moved on and the stamp
	// never will.
	next, ok := m.loadDiffCmd(false)().(diffLoadedMsg)
	if !ok {
		t.Fatal("loadDiffCmd did not produce a diffLoadedMsg")
	}
	if next.themeGen != m.themeGen {
		t.Fatalf("after the switch a load is stamped %d but the model is at %d — every diff would be dropped",
			next.themeGen, m.themeGen)
	}
	updated, _ = m.Update(next)
	m = updated.(Model)
	if m.renderer == before {
		t.Error("a diff built under the current palette was dropped")
	}
}

// applyTheme has to rebuild everything derived from the palette, not just set
// the field. Deleting the Styles rebuild, and deleting the cache reset, were
// both green: the model would claim the new theme while every row on screen
// kept the old one's colours.
func TestThemePicker_ApplyingAThemeRebuildsWhatIsDerivedFromIt(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")
	m := liveModel(t, tr)
	m.lastDiffContent = "stale content from the previous palette"

	// Styles holds lipgloss.Style values, which are not comparable — and
	// Render tells you nothing here, because under go test lipgloss emits no
	// escapes at all. What the style *stores* is the observable thing.
	gruvbox := theme.Themes["gruvbox"]
	if m.styles.DiffAdded.GetBackground() == lipgloss.Color(gruvbox.AddedBg) {
		t.Fatal("the model already uses gruvbox, so this test proves nothing")
	}

	m, cmd := m.applyTheme(gruvbox)

	if m.styles.DiffAdded.GetBackground() != lipgloss.Color(gruvbox.AddedBg) {
		t.Errorf("DiffAdded still paints %v, not gruvbox's %s",
			m.styles.DiffAdded.GetBackground(), gruvbox.AddedBg)
	}
	if m.lastDiffContent != "" {
		t.Error("the cached diff was kept, so the rows would keep the old colours")
	}
	if cmd == nil {
		t.Error("nothing was scheduled to re-render the diff")
	}
}

// The file-list v handler is now the only thing keeping cfg.SplitDiff correct
// on that path, because saveSplitPrefCmd writes m.cfg verbatim. Removing it was
// green: the existing test drives updateDiffMode only.
func TestThemePicker_TheFileListSplitToggleAlsoKeepsTheConfigCurrent(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")

	for _, tc := range []struct {
		name string
		call func(Model) (tea.Model, tea.Cmd)
	}{
		{"file list", func(m Model) (tea.Model, tea.Cmd) { return m.updateFileListMode(key("v")) }},
		{"diff", func(m Model) (tea.Model, tea.Cmd) { return m.updateDiffMode(key("v")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := liveModel(t, tr)
			before := m.splitDiff

			updated, _ := tc.call(m)
			m = updated.(Model)

			if m.splitDiff == before {
				t.Fatal("v did not toggle split view")
			}
			if m.cfg.SplitDiff != m.splitDiff {
				t.Errorf("cfg.SplitDiff is %v but the session is %v — a theme write would revert it",
					m.cfg.SplitDiff, m.splitDiff)
			}
		})
	}
}

// Every key that closes the picker. Narrowing the case to "esc" alone was
// green, so the documented toggle — press t again to dismiss — had no test.
func TestThemePicker_EveryClosingKeyCloses(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")

	for _, k := range []string{"esc", "q", "t"} {
		t.Run(k, func(t *testing.T) {
			t.Parallel()
			m := liveModel(t, tr)
			before := m.theme
			m, _ = m.openThemePicker()
			m, _ = m.moveThemeCursor(1)

			m, _, handled := m.themePickerKey(k)
			if !handled {
				t.Fatalf("%q was not handled by the picker", k)
			}
			if m.showThemes {
				t.Errorf("%q did not close the picker", k)
			}
			if m.theme != before {
				t.Errorf("%q closed the picker without restoring the theme", k)
			}
		})
	}
}

// Opening the picker has to close the other overlays. Without it, ! then t
// leaves the problem overlay on screen — View() checks showProblem before the
// branch that draws the picker — while the picker silently owns the keyboard
// and swallows every key. The screen looks frozen.
func TestThemePicker_OpeningItClosesTheOtherOverlays(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")
	m := liveModel(t, tr)

	m.showProblem, m.showHelp, m.showHistory = true, true, true
	m, _ = m.openThemePicker()

	if m.showProblem || m.showHelp || m.showHistory {
		t.Errorf("another overlay survived: problem=%v help=%v history=%v",
			m.showProblem, m.showHelp, m.showHistory)
	}
	if !strings.Contains(m.View(), "theme") {
		t.Errorf("the picker is not what is on screen:\n%s", m.View())
	}
}

// The cursor stops at the ends rather than wrapping.
func TestThemePicker_TheCursorClampsAtBothEnds(t *testing.T) {
	t.Parallel()
	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")
	m := liveModel(t, tr)
	m, _ = m.openThemePicker()

	m.themeCursor = 0
	m, _ = m.moveThemeCursor(-1)
	if m.themeCursor != 0 {
		t.Errorf("up from the first theme went to %d, not staying at 0", m.themeCursor)
	}

	last := len(theme.ThemeNames()) - 1
	m.themeCursor = last
	m, _ = m.moveThemeCursor(1)
	if m.themeCursor != last {
		t.Errorf("down from the last theme went to %d, not staying at %d", m.themeCursor, last)
	}
}

// Confirming has to reach the disk. "Enter persists the choice" was asserted
// only as cmd != nil, which applyTheme's reload satisfies on its own — so
// removing the config write entirely left the suite green, and the issue's
// "used on next launch" criterion was untested.
//
// Not t.Parallel: it points HOME at a temp dir, which t.Setenv forbids in a
// parallel test. That isolation is the point — without it this test would
// rewrite the developer's own ~/.config/differ/config.json, which is a hazard
// every test that executes a returned Cmd has been carrying.
func TestThemePicker_ConfirmingWritesTheThemeToDisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tr := testutil.NewRepo(t)
	tr.CommitFile("src.ts", "one\n", "first")
	tr.Modify("src.ts", "two\n")
	m := liveModel(t, tr)

	m, _ = m.openThemePicker()
	m, _ = m.moveThemeCursor(1)
	want := theme.ThemeNames()[m.themeCursor]

	m, cmd := m.confirmTheme()
	if cmd == nil {
		t.Fatal("enter returned no command at all")
	}
	drain(cmd)

	path := filepath.Join(home, ".config", "differ", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("enter did not write a config: %v", err)
	}
	var saved config.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("the config it wrote is not valid JSON: %v", err)
	}
	if saved.Theme != want {
		t.Errorf("the config on disk says theme=%q, want %q", saved.Theme, want)
	}
}

// drain runs a command and everything a tea.Batch fans out to.
func drain(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			drain(c)
		}
	}
}
