package ui

import (
	tea "github.com/charmbracelet/bubbletea"
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
	initChromaStyle(t.ChromaStyle)
	m.lastDiffContent = ""
	return m, m.loadDiffCmd(false)
}

// confirmTheme keeps the previewed theme and remembers it for next time.
func (m Model) confirmTheme() (Model, tea.Cmd) {
	m.showThemes = false
	name := theme.ThemeNames()[m.themeCursor]
	m.cfg.Theme = name
	m.statusMsg = "theme: " + name

	cfg := m.cfg
	return m, func() tea.Msg { return savePrefDoneMsg{err: config.Save(cfg)} }
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
func (m Model) renderThemeOverlay(width, height int) string {
	names := theme.ThemeNames()
	rows := make([]string, 0, len(names))
	for i, name := range names {
		label := "  " + name
		if i == m.themeCursor {
			label = m.styles.Accent.Render(focusBar) + m.styles.PanelLabelFocus.Render(" "+name)
		}
		if name == m.cfg.Theme {
			label += m.styles.HelpDesc.Render("   in use")
		}
		rows = append(rows, label+"  "+m.themeSwatch(theme.Themes[name]))
	}
	return m.fitOverlay(" theme", rows, "j/k to try · enter to keep · esc to cancel", width, height)
}

// themeSwatch shows a theme's diff colours, which is what the eye is actually
// choosing between.
func (m Model) themeSwatch(t theme.Theme) string {
	s := NewStyles(t)
	return s.DiffAdded.Render(" + added ") + s.DiffRemoved.Render(" - removed ") +
		s.DiffContext.Render(" context ")
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
