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
	// One question — "did anything move?" — instead of eight answers nobody
	// asked for. The refresh happens in handleRepoProbed, and only if it did.
	//
	// Not while one is already out: tea.Tick does not wait for the previous
	// command, so on a repository where git status takes longer than the
	// interval the probes would pile up, each one contending for the index
	// with the last.
	m.ticksSinceRefresh++
	if m.probing {
		// A probe that never comes back would otherwise wedge the poll loop
		// for good — git status on a hung mount does not error, it blocks —
		// and nothing on screen would say so. After staleProbeTicks the flag
		// is dropped and a fresh one goes out; the stale answer is harmless
		// when it eventually lands, because it is only a fingerprint.
		m.probeWaited++
		if m.probeWaited < staleProbeTicks {
			return m, tickCmd()
		}
	}
	m.probing = true
	m.probeWaited = 0
	return m, tea.Batch(m.probeCmd(), tickCmd())
}

// refreshEvery is the most often the expensive rebuild runs, in ticks.
//
// The probe is cheap and runs every tick, so a change is noticed within a
// second. Acting on it is rate-limited: while an agent writes continuously
// every probe would move, and a refresh a second is nine git processes a
// second — more churn than the two-second rebuild this replaced, in exactly
// the burst the issue is about. This is the coalescing it asks for: many
// writes in quick succession become one refresh, not twenty.
const refreshEvery = 2

// staleProbeTicks is how long a probe may be out before another is sent.
const staleProbeTicks = 10

// probeCmd asks git for the repository's fingerprint, off the update loop.
func (m Model) probeCmd() tea.Cmd {
	repo := m.repo
	ref := m.ref
	return func() tea.Msg {
		fingerprint, err := repo.Probe(ref)
		return repoProbedMsg{fingerprint: fingerprint, err: err}
	}
}

// handleRepoProbed does the expensive work, but only when the probe says the
// repository actually moved.
//
// A burst of writes is coalesced by the interval itself: whatever an agent does
// between two probes becomes one refresh, however many files it touched.
func (m Model) handleRepoProbed(msg repoProbedMsg) (tea.Model, tea.Cmd) {
	m.probing = false
	if msg.err != nil {
		// The probe is an optimisation, never a gate. A repository mid-rebase,
		// a vanished git binary or an unreadable index must cost the user a
		// wasted refresh, not a screen that has quietly stopped updating.
		//
		// Rate-limited like any other refresh, though: a repository where the
		// probe reliably fails would otherwise rebuild every tick — nine
		// processes a second, in exactly the degraded state where that is
		// least welcome.
		if m.ticksSinceRefresh < refreshEvery {
			return m, nil
		}
		m.ticksSinceRefresh = 0
		m.refreshSeq++
		return m, tea.Batch(m.refreshFilesAs("", m.refreshSeq), m.fetchUpstreamStatusCmd())
	}
	// A refresh must not land under an open input. The tick already declines
	// to probe in those modes, but a probe dispatched a moment earlier can
	// arrive after the user has pressed c or b — and handleFilesRefreshed
	// reorders the file list, moves the cursor and reloads the diff.
	if m.mode == modeCommit || m.mode == modeBranchPicker || m.generatingMsg {
		return m, nil
	}
	if msg.fingerprint == m.repoFingerprint {
		return m, nil
	}
	if m.ticksSinceRefresh < refreshEvery {
		// Seen, but not acted on yet. The fingerprint is deliberately not
		// stored: the next probe must still find a difference, or this change
		// would be dropped rather than delayed.
		return m, nil
	}
	// Still not stored here. It is a claim that the screen matches the
	// repository, and nothing on screen has changed yet — handleFilesRefreshed
	// stores it when the rebuild actually lands. Storing it on dispatch made a
	// failed or out-of-order refresh permanent: the screen kept its old state
	// while the fingerprint said it was current, so no later probe would ever
	// disagree.
	m.ticksSinceRefresh = 0
	m.refreshSeq++
	return m, tea.Batch(m.refreshFilesAs(msg.fingerprint, m.refreshSeq), m.fetchUpstreamStatusCmd())
}

// refreshEverythingCmd is what a tick used to do unconditionally.
func (m Model) refreshEverythingCmd() tea.Cmd {
	return tea.Batch(m.refreshFilesCmd(), m.fetchUpstreamStatusCmd())
}

func (m Model) handlePushDone(msg pushDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.fail("push", msg.err), nil
	}
	m.statusMsg = "pushed!"
	return m, m.fetchUpstreamStatusCmd()
}

func (m Model) handlePullDone(msg pullDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.fail("pull", msg.err), nil
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
	// Split view needs the two-panel layout as well as the width. Without that
	// it engaged between 60 and 71 columns — where the layout has collapsed and
	// the diff briefly has the whole terminal — and then switched off at 72
	// when the file list reappeared and cut the diff to 45. Widening a pane by
	// one column dropped the user out of split view, which is the opposite of
	// what the README promises.
	splitMode := m.splitDiff && !m.onePanel() && diffW >= minSplitWidth
	tabWidth := m.cfg.TabWidth
	gen := m.themeGen
	return func() tea.Msg {
		fail := func(err error) tea.Msg {
			return diffLoadedMsg{
				errContent:  styles.DiffHunkHeader.Render("Error: " + err.Error()),
				index:       idx,
				resetScroll: resetScroll,
				themeGen:    gen,
			}
		}

		var parsed ParsedDiff
		if f.untracked {
			raw, err := repo.ReadFileContent(filename)
			if err != nil {
				return fail(err)
			}
			parsed = ParseNewFile(raw)
		} else {
			raw, err := repo.DiffFile(filename, staged, ref)
			if err != nil {
				return fail(err)
			}
			parsed = ParseDiff(raw)
		}

		r := NewDiffRenderer(parsed, filename, styles, t, diffW)
		r.SetTabWidth(tabWidth)
		r.SetSplit(splitMode)
		return diffLoadedMsg{renderer: r, index: idx, resetScroll: resetScroll, themeGen: gen}
	}
}

func (m Model) refreshFilesCmd() tea.Cmd {
	return m.refreshFilesAs("", m.refreshSeq)
}

// refreshFilesAs rebuilds the file list, tagged with the fingerprint the
// repository had when it was asked for and with its place in the queue.
//
// An explicit refresh — staging, committing, switching branch — passes an
// empty fingerprint: it knows the repository changed but not what it looks
// like now, so it installs its files without claiming the screen is current.
func (m Model) refreshFilesAs(fingerprint string, seq int) tea.Cmd {
	repo := m.repo
	stagedOnly := m.stagedOnly
	ref := m.ref
	return func() tea.Msg {
		fail := func(err error) tea.Msg {
			return filesRefreshedMsg{err: err, seq: seq}
		}
		files, err := repo.ChangedFiles(stagedOnly, ref)
		if err != nil {
			return fail(err)
		}
		var untracked []string
		if !stagedOnly && ref == "" {
			untracked, err = repo.UntrackedFiles()
			if err != nil {
				return fail(err)
			}
		}
		items := buildFileItems(repo, files, untracked)
		return filesRefreshedMsg{
			files:       items,
			keys:        fileKeysOf(repo, items, stagedOnly),
			fingerprint: fingerprint,
			seq:         seq,
		}
	}
}

func (m Model) buildRefreshedFiles() filesRefreshedMsg {
	files, err := m.repo.ChangedFiles(m.stagedOnly, m.ref)
	if err != nil {
		return filesRefreshedMsg{err: err}
	}
	var untracked []string
	if !m.stagedOnly && m.ref == "" {
		untracked, err = m.repo.UntrackedFiles()
		if err != nil {
			return filesRefreshedMsg{err: err}
		}
	}
	items := buildFileItems(m.repo, files, untracked)
	return filesRefreshedMsg{files: items, keys: fileKeysOf(m.repo, items, m.stagedOnly)}
}

// saveSplitPrefCmd persists the split preference.
//
// m.cfg is the one copy that gets written, by this and by the theme picker.
// This used to mutate a local copy instead, so m.cfg.SplitDiff kept its
// startup value for the whole session and the next whole-config write — a
// theme change — silently put the old value back.
func (m Model) saveSplitPrefCmd() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg { return savePrefDoneMsg{err: config.Save(cfg)} }
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

// rerenderCmd rebuilds the diff on screen at the current width and palette,
// from the parse already in hand rather than from the file.
//
// It keeps the key the content was read with, so a resize or a theme change
// cannot mark a held diff as current — which would remove the notice without
// the reviewer ever seeing what moved.
func (m Model) rerenderCmd() tea.Cmd {
	if m.renderer == nil {
		return nil
	}
	parsed := m.renderer.Parsed()
	idx := m.cursor
	styles := m.styles
	t := m.theme
	diffW := m.diffWidth()
	filename := m.rendererPath
	splitMode := m.splitDiff && diffW >= minSplitWidth
	tabWidth := m.cfg.TabWidth
	key := m.rendererKey
	gen := m.themeGen
	return func() tea.Msg {
		r := NewDiffRenderer(parsed, filename, styles, t, diffW)
		r.SetTabWidth(tabWidth)
		r.SetSplit(splitMode)
		return diffLoadedMsg{renderer: r, index: idx, resetScroll: false, key: key, themeGen: gen}
	}
}
