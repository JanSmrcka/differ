package ui

import (
	"strings"
	"testing"
)

// #48: every question is the same shape. Help, history and the problem used
// to replace the whole panel area, and the commit message was a footer bar —
// four shapes beside the three modals. Now each is a box over the view, and
// the frame around it (header, rules, bar) is still there.
func TestVisual_EveryQuestionIsABox(t *testing.T) {
	const w, h = 120, 30
	for _, c := range []struct {
		name  string
		open  func(Model) Model
		title string
	}{
		{"help", func(m Model) Model { m.showHelp = true; return m }, "keys"},
		{"history", func(m Model) Model { m.showHistory = true; return m }, "already sent"},
		{"problem", func(m Model) Model { m.showProblem = true; return m }, "last problem"},
		{"commit", func(m Model) Model {
			m.mode = modeCommit
			m.commitInput.Focus()
			return m
		}, "commit"},
		{"branch picker", func(m Model) Model {
			u, _ := m.handleBranchesLoaded(branchesLoadedMsg{branches: []string{"master", "x"}, current: "master"})
			return u.(Model)
		}, "branch"},
		{"agent picker", func(m Model) Model { m.showAgents = true; return m }, "agent"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := c.open(chromeModel(t, w, h))
			view := stripANSI(m.View())
			rows := strings.Split(view, "\n")
			if len(rows) != h {
				t.Fatalf("%d rows, want %d", len(rows), h)
			}
			if !strings.Contains(view, "╭") || !strings.Contains(view, "╯") {
				t.Errorf("not drawn as a box:\n%s", view)
			}
			if !strings.Contains(rows[0], "differ") {
				t.Errorf("the header is gone: %q", rows[0])
			}
			if strings.ContainsAny(rows[h-1], "╭╮╰╯│") {
				t.Errorf("the box covers the bar: %q", rows[h-1])
			}
			if !strings.Contains(view, c.title) {
				t.Errorf("the box has no %q title:\n%s", c.title, view)
			}
		})
	}
}

// The view stays visible around a reading overlay: you look something up
// about the screen you are on.
func TestVisual_TheViewShowsAroundTheHelp(t *testing.T) {
	m := chromeModel(t, 120, 30)
	m.showHelp = true
	if view := stripANSI(m.View()); !strings.Contains(view, "Files") {
		t.Errorf("the help box hid the whole view:\n%s", view)
	}
}
