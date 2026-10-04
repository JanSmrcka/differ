package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jansmrcka/differ/internal/config"
	"github.com/jansmrcka/differ/internal/theme"
)

// The theme picker.
//
// Trying a theme used to mean editing a config file or restarting with
// --theme, which is a poor way to find out whether you like one. Moving the
// selection repaints the screen in that theme, so the answer is in front of
// you; enter keeps it, esc puts back what was there and writes nothing.

// openThemePicker remembers what is in use, so cancelling can restore it, and
// starts on it rather than at the top of the list.
func (m Model) openThemePicker() (Model, tea.Cmd) {
	m.showThemes = true
	m.showHelp, m.showHistory, m.showProblem = false, false, false
	m.themeBefore = m.theme
	m.themeCursor = 0
	for i, name := range theme.ThemeNames() {
		if theme.Themes[name] == m.theme {
			m.themeCursor = i
			break
		}
	}
	return m, nil
}

// moveThemeCursor previews the theme it lands on.
func (m Model) moveThemeCursor(delta int) (Model, tea.Cmd) {
	names := theme.ThemeNames()
	m.themeCursor = clampCursor(m.themeCursor+delta, len(names))
	return m.applyTheme(theme.Themes[names[m.themeCursor]])
}

// applyTheme swaps the palette and everything derived from it.
//
// The diff has to be re-rendered rather than recoloured: DiffRenderer caches
// each row as a finished string, with the styles it was built from baked in.
func (m Model) applyTheme(t theme.Theme) (Model, tea.Cmd) {
	m.theme = t
	m.styles = NewStyles(t)
	m.lastDiffContent = ""
	// Every in-flight diff was built with the old palette. The counter lets
	// their results be dropped rather than installed after this one.
	m.themeGen++
	// Same as a resize, guard included: on a diff that is not held
	// diffStale() is briefly true between a refresh installing new keys and
	// the load it batched landing, and re-rendering there would keep old
	// content nobody asked to keep.
	if m.holdsTheDiff() && m.diffStale() {
		return m, m.rerenderCmd()
	}
	return m, m.loadDiffCmd(false)
}

// confirmTheme keeps the previewed theme and remembers it for next time.
//
// It applies the selection as well as writing it. Writing without applying
// meant enter with no movement at all persisted whatever the cursor had landed
// on — and the cursor falls back to the first theme when the one in use is not
// in the registry, which is exactly the case under --no-color.
func (m Model) confirmTheme() (Model, tea.Cmd) {
	m.showThemes = false
	name := theme.ThemeNames()[m.themeCursor]

	m, cmd := m.applyTheme(theme.Themes[name])
	m.cfg.Theme = name
	m.statusMsg = "theme: " + name

	cfg := m.cfg
	save := func() tea.Msg { return savePrefDoneMsg{err: config.Save(cfg)} }
	return m, tea.Batch(cmd, save)
}

// cancelTheme puts back what was in use and writes nothing.
func (m Model) cancelTheme() (Model, tea.Cmd) {
	m.showThemes = false
	if m.theme == m.themeBefore {
		return m, nil
	}
	return m.applyTheme(m.themeBefore)
}

// renderThemeOverlay lists the themes with the selection marked.
//
// It takes the file list's panel rather than the whole content area, because
// the point of the picker is to see the diff in the theme it is offering. Over
// everything it was previewing the chrome and the swatches and nothing else,
// which the issue asks for by name: "moving the selection re-renders the
// current screen in that theme".
func (m Model) renderThemeOverlay(width, height int) string {
	names := theme.ThemeNames()
	rows := make([]string, 0, len(names))
	for i, name := range names {
		label := "  " + name
		if i == m.themeCursor {
			label = m.styles.Accent.Render(focusBar) + m.styles.PanelLabelFocus.Render(" "+name)
		}
		// By palette, not by name: the config can say "dark", which is an
		// alias and not in ThemeNames, and --theme overrides the config
		// entirely — so a name comparison marked nothing at all on a default
		// config, and the wrong row under --theme.
		if theme.Themes[name] == m.themeBefore {
			label += m.styles.HelpDesc.Render("  ·  in use")
		}
		rows = append(rows, label+" "+m.themeSwatch(theme.Themes[name], width-lipgloss.Width(label)-2))
	}
	// The closing line is the one thing fitOverlay always keeps, so it says
	// less rather than being cut: the file-list panel is 24 columns at 80, and
	// "j/k · enter keeps · esc cancels" arrived as "j/k · enter keeps · es…".
	closing := "j/k · enter keeps · esc cancels"
	if width < lipgloss.Width(closing)+2 {
		closing = "j/k · enter · esc"
	}
	if width < lipgloss.Width(closing)+2 {
		closing = "enter · esc"
	}
	return m.fitOverlay(" theme", rows, closing, width, height)
}

// themeSwatch shows a theme's diff colours, in whatever room is left.
func (m Model) themeSwatch(t theme.Theme, room int) string {
	s := NewStyles(t)
	if room < 9 {
		return ""
	}
	return s.DiffAdded.Render(" + ") + s.DiffRemoved.Render(" - ") + s.DiffContext.Render(" ctx ")
}

// themePickerKey handles the picker's keys while it is open.
func (m Model) themePickerKey(key string) (Model, tea.Cmd, bool) {
	switch key {
	case "j", "down":
		mm, cmd := m.moveThemeCursor(1)
		return mm, cmd, true
	case "k", "up":
		mm, cmd := m.moveThemeCursor(-1)
		return mm, cmd, true
	case "enter":
		mm, cmd := m.confirmTheme()
		return mm, cmd, true
	case "esc", "q", "t":
		mm, cmd := m.cancelTheme()
		return mm, cmd, true
	}
	// Everything else is swallowed: the screen behind the picker is being
	// repainted, and a stray key acting on it would be acting on a theme the
	// user has not chosen yet.
	return m, nil, true
}
