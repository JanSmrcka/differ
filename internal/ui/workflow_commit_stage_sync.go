package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/config"
)

// Commit, staging, polling, sync, and async command workflows.

func (m Model) updateCommitMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeFileList
		m.commitInput.Reset()
		return m, nil
	case "enter":
		message := m.commitInput.Value()
		if strings.TrimSpace(message) == "" {
			m.statusMsg = "empty commit message"
			return m, nil
		}
		return m, m.commitCmd(message)
	}
	var cmd tea.Cmd
	m.commitInput, cmd = m.commitInput.Update(msg)
	return m, cmd
}

func (m Model) toggleStage() (tea.Model, tea.Cmd) {
	if m.stagedOnly || m.ref != "" || len(m.files) == 0 {
		return m, nil
	}
	f := m.files[m.cursor]
	repo := m.repo
	path := f.change.Path
	return m, func() tea.Msg {
		if f.change.Staged {
			_ = repo.UnstageFile(path)
		} else {
			_ = repo.StageFile(path)
		}
		return m.buildRefreshedFiles()
	}
}

func (m Model) stageAll() (tea.Model, tea.Cmd) {
	if m.stagedOnly || m.ref != "" {
		return m, nil
	}
	repo := m.repo
	return m, func() tea.Msg {
		_ = repo.StageAll()
		return m.buildRefreshedFiles()
	}
}

func (m Model) enterCommitMode() (tea.Model, tea.Cmd) {
	if m.ref != "" {
		return m, nil
	}
	hasStaged := false
	for _, f := range m.files {
		if f.change.Staged {
			hasStaged = true
			break
		}
	}
	if !hasStaged {
		m.statusMsg = "no staged files"
		return m, nil
	}
	m.mode = modeCommit
	m.generatingMsg = true
	m.statusMsg = "generating commit message..."
	m.commitInput.Focus()
	return m, tea.Batch(textinput.Blink, m.generateCommitMsgCmd())
}

func (m Model) fetchUpstreamStatusCmd() tea.Cmd {
	repo := m.repo
	return func() tea.Msg { return upstreamStatusMsg{info: repo.UpstreamStatus()} }
}

func (m Model) pushCmd() tea.Cmd {
	repo := m.repo
	return func() tea.Msg { return pushDoneMsg{err: repo.Push()} }
}

func (m Model) pushSetUpstreamCmd() tea.Cmd {
	repo := m.repo
	branch := m.currentBranch
	if branch == "" {
		branch = repo.BranchName()
	}
	return func() tea.Msg { return pushDoneMsg{err: repo.PushSetUpstream("origin", branch)} }
}

func (m Model) pullCmd() tea.Cmd {
	repo := m.repo
	return func() tea.Msg { return pullDoneMsg{err: repo.Pull()} }
}

func tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) handleTick() (tea.Model, tea.Cmd) {
	if m.mode == modeCommit || m.mode == modeBranchPicker || m.generatingMsg {
		return m, tickCmd()
	}
	return m, tea.Batch(m.refreshFilesCmd(), m.fetchUpstreamStatusCmd(), tickCmd())
}

func (m Model) handlePushDone(msg pushDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = "push failed: " + msg.err.Error()
		return m, nil
	}
	m.statusMsg = "pushed!"
	return m, m.fetchUpstreamStatusCmd()
}

func (m Model) handlePullDone(msg pullDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = "pull failed: " + msg.err.Error()
		return m, nil
	}
	m.statusMsg = "pulled!"
	return m, tea.Batch(m.refreshFilesCmd(), m.fetchUpstreamStatusCmd())
}

func (m Model) loadDiffCmd(resetScroll bool) tea.Cmd {
	if len(m.files) == 0 {
		return nil
	}
	idx := m.cursor
	f := m.files[idx]
	repo := m.repo
	styles := m.styles
	t := m.theme
	staged := f.change.Staged
	ref := m.ref
	diffW := m.diffWidth()
	filename := f.change.Path
	splitMode := m.splitDiff && diffW >= minSplitWidth
	return func() tea.Msg {
		var content string
		if f.untracked {
			raw, err := repo.ReadFileContent(filename)
			if err != nil {
				content = styles.DiffHunkHeader.Render("Error: " + err.Error())
			} else if splitMode {
				content = RenderNewFileSplit(raw, filename, styles, t, diffW)
			} else {
				content = RenderNewFile(raw, filename, styles, t, diffW)
			}
		} else {
			raw, err := repo.DiffFile(filename, staged, ref)
			if err != nil {
				content = styles.DiffHunkHeader.Render("Error: " + err.Error())
			} else {
				parsed := ParseDiff(raw)
				if splitMode {
					content = RenderSplitDiff(parsed, filename, styles, t, diffW)
				} else {
					content = RenderDiff(parsed, filename, styles, t, diffW)
				}
			}
		}
		return diffLoadedMsg{content: content, index: idx, resetScroll: resetScroll}
	}
}

func (m Model) refreshFilesCmd() tea.Cmd {
	repo := m.repo
	stagedOnly := m.stagedOnly
	ref := m.ref
	return func() tea.Msg {
		files, _ := repo.ChangedFiles(stagedOnly, ref)
		var untracked []string
		if !stagedOnly && ref == "" {
			untracked, _ = repo.UntrackedFiles()
		}
		return filesRefreshedMsg{files: buildFileItems(repo, files, untracked)}
	}
}

func (m Model) buildRefreshedFiles() filesRefreshedMsg {
	files, _ := m.repo.ChangedFiles(m.stagedOnly, m.ref)
	var untracked []string
	if !m.stagedOnly && m.ref == "" {
		untracked, _ = m.repo.UntrackedFiles()
	}
	return filesRefreshedMsg{files: buildFileItems(m.repo, files, untracked)}
}

func (m Model) saveSplitPrefCmd() tea.Cmd {
	cfg := m.cfg
	split := m.splitDiff
	return func() tea.Msg {
		cfg.SplitDiff = split
		return savePrefDoneMsg{err: config.Save(cfg)}
	}
}

func (m Model) commitCmd(message string) tea.Cmd {
	repo := m.repo
	return func() tea.Msg { return commitDoneMsg{err: repo.Commit(message)} }
}

const defaultCommitMsgCmd = "claude -p"
const defaultCommitMsgPrompt = "Write a concise git commit message (one line, no quotes, use conventional commit prefixes like feat:, fix:, chore:, refactor: etc when appropriate) for this diff:"

func (m Model) generateCommitMsgCmd() tea.Cmd {
	repo := m.repo
	cfg := m.cfg
	return func() tea.Msg {
		diff, err := repo.StagedDiff()
		if err != nil {
			return commitMsgGeneratedMsg{err: fmt.Errorf("git diff: %w", err)}
		}
		if strings.TrimSpace(diff) == "" {
			return commitMsgGeneratedMsg{err: fmt.Errorf("empty staged diff")}
		}
		const maxDiff = 8000
		if len(diff) > maxDiff {
			diff = diff[:maxDiff] + "\n... (truncated)"
		}
		promptPrefix := defaultCommitMsgPrompt
		if cfg.CommitMsgPrompt != "" {
			promptPrefix = cfg.CommitMsgPrompt
		}
		prompt := promptPrefix + "\n\n" + diff
		cmdStr := defaultCommitMsgCmd
		if cfg.CommitMsgCmd != "" {
			cmdStr = cfg.CommitMsgCmd
		}
		parts := strings.Fields(cmdStr)
		args := append(parts[1:], prompt)
		cmd := exec.Command(parts[0], args...)
		out, err := cmd.Output()
		if err != nil {
			return commitMsgGeneratedMsg{err: fmt.Errorf("%s: %w", parts[0], err)}
		}
		return commitMsgGeneratedMsg{message: strings.TrimSpace(string(out))}
	}
}
