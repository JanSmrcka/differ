package ui

import (
	"context"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/editor"
)

// Opening the file under the cursor in an editor, without ending differ.
//
// Deciding how to open it reads the filesystem and may talk to tmux, so it
// never runs in Update — it is a tea.Cmd whose result comes back as
// editorPlanMsg. Only then does the UI act, because handing the terminal to a
// child process is something only the program can do.

// editorPlanMsg carries the decided plan, or why there is none.
type editorPlanMsg struct {
	plan editor.Plan
	err  error
}

// editorDoneMsg reports that the editor finished, or was handed the file.
type editorDoneMsg struct {
	desc string
	err  error
}

// openFileInEditor is the shared `e` handler. Both the file list and the diff call
// it, so the key means the same thing in both.
func (m Model) openFileInEditor() (tea.Model, tea.Cmd) {
	path := m.currentFilePath()
	if path == "" {
		m.statusMsg = "no file selected"
		return m, nil
	}
	req := editor.Request{
		File: path,
		Repo: m.repo.Dir(),
		Line: m.editorLine(),
		Env:  editor.NewEnv(),
	}
	cfg := editor.Config{Cmd: m.cfg.EditorCmd}
	return m, func() tea.Msg {
		plan, err := editor.Resolve(context.Background(), cfg, req)
		return editorPlanMsg{plan: plan, err: err}
	}
}

// editorLine is the line in the file on disk that the cursor is looking at.
//
// The diff cursor can sit on a removed line, which has no counterpart on
// disk; the nearest preceding line that does have one puts the editor right
// above the code the reviewer was reading. Returns 0 when there is nothing to
// aim at, as in the file list.
func (m Model) editorLine() int {
	// Only the diff and review modes have a cursor that means a line; the
	// file list just shows a preview.
	if m.mode != modeDiff && m.mode != modeReview {
		return 0
	}
	// Diffs load asynchronously, so the renderer on screen may still be the
	// previous file's — after entering the diff, or after n/p, until
	// diffLoadedMsg arrives. A line taken from it would be about the wrong
	// file, so claim none until the two agree.
	if m.renderer == nil || m.rendererPath != m.currentFilePath() {
		return 0
	}

	parsed := m.renderer.Parsed()
	if m.diffCursor < 0 || m.diffCursor >= len(parsed.Lines) {
		return 0
	}
	// A hunk header belongs to the hunk it introduces, not to whatever came
	// before it. Scanning back from one would walk into the previous hunk and
	// return its last line, which can be a long way from what is on screen.
	if parsed.Lines[m.diffCursor].Type == LineHunkHeader {
		if h, ok := parsed.HunkAt(m.diffCursor); ok {
			return h.NewStart
		}
		return 0
	}
	for i := m.diffCursor; i >= 0; i-- {
		if n := parsed.Lines[i].NewNum; n > 0 {
			return n
		}
	}
	if h, ok := parsed.HunkAt(m.diffCursor); ok {
		return h.NewStart
	}
	return 0
}

func (m Model) handleEditorPlan(msg editorPlanMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = msg.err.Error()
		return m, nil
	}
	// Every plan so far needs the terminal, and only the program can hand it
	// over — hence ExecProcess rather than running the command ourselves.
	plan := msg.plan
	cmd := exec.Command(plan.Argv[0], plan.Argv[1:]...)
	cmd.Dir = plan.Dir
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{desc: plan.Desc, err: err}
	})
}

func (m Model) handleEditorDone(msg editorDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = "editor failed: " + msg.err.Error()
	} else if msg.desc != "" {
		m.statusMsg = msg.desc
	}
	// The file may have changed under us. Reload without resetting, so the
	// reviewer comes back to the file and position they left — the two-second
	// poll would get there eventually, but not before the screen has looked
	// stale.
	return m, tea.Batch(m.refreshFilesCmd(), m.loadDiffCmd(false))
}
