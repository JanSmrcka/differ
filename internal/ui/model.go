package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/config"
	"github.com/jansmrcka/differ/internal/feedback"
	"github.com/jansmrcka/differ/internal/git"
	"github.com/jansmrcka/differ/internal/review"
	"github.com/jansmrcka/differ/internal/theme"
)

type viewMode int

const (
	modeFileList viewMode = iota
	modeDiff
	modeCommit
	modeBranchPicker
	modeReview
)

const fileListWidth = 35
const pollInterval = 2 * time.Second

const (
	minWidth  = 60
	minHeight = 10
)

type tickMsg time.Time

type diffLoadedMsg struct {
	// renderer is nil when the diff could not be loaded; errContent then holds
	// the message to show instead.
	renderer    *DiffRenderer
	errContent  string
	index       int
	resetScroll bool
}

type filesRefreshedMsg struct{ files []fileItem }
type commitDoneMsg struct{ err error }

// feedbackSentMsg reports the outcome of a delivery attempt. ids names the
// comments that were in the payload, so they are marked sent only on success.
type feedbackSentMsg struct {
	ids    []string
	target string
	err    error
}

type commitMsgGeneratedMsg struct {
	message string
	err     error
}

type branchesLoadedMsg struct {
	branches []string
	current  string
	err      error
}

type branchSwitchedMsg struct{ err error }

type upstreamStatusMsg struct{ info git.UpstreamInfo }
type pushDoneMsg struct{ err error }
type pullDoneMsg struct{ err error }
type savePrefDoneMsg struct{ err error }

type branchCreatedMsg struct {
	name string
	err  error
}

// Model holds all UI state; behavior split across focused files.
type Model struct {
	repo       *git.Repo
	cfg        config.Config
	files      []fileItem
	styles     Styles
	theme      theme.Theme
	stagedOnly bool
	ref        string

	mode          viewMode
	cursor        int
	prevCurs      int
	viewport      viewport.Model
	commitInput   textinput.Model
	statusMsg     string
	generatingMsg bool
	splitDiff     bool
	width         int
	height        int
	ready         bool
	SelectedFile  string

	lastDiffContent string

	// renderer holds the diff currently on screen; diffCursor indexes into its
	// lines and is the anchor review comments will attach to.
	renderer   *DiffRenderer
	diffCursor int
	// cursorPlaced records that the cursor has been positioned for the
	// current diff, so a resize preserves it but the first load still lands
	// on the first reviewable line.
	cursorPlaced bool

	branches         []string
	filteredBranches []string
	branchCursor     int
	branchOffset     int
	currentBranch    string
	branchFilter     textinput.Model
	branchCreating   bool
	branchInput      textinput.Model

	upstream    git.UpstreamInfo
	pushConfirm bool
	quitConfirm bool

	// commenting is true while the comment editor is open; draft is the
	// comment being written, and editingID is set when editing an existing
	// one rather than creating a new comment.
	commenting   bool
	draft        review.Comment
	editingID    string
	commentInput textarea.Model

	// target delivers review feedback; targetErr records why it could not be
	// built, so the problem is reported when the user tries to send rather
	// than at startup.
	target    feedback.Target
	targetErr error

	// session holds review state — comments and per-file progress. It is
	// created on first entering review mode and lives until the process ends;
	// it is never written to disk and never mirrored into the git index.
	session *review.Session
}

type fileItem struct {
	change    git.FileChange
	untracked bool
}

func NewModel(repo *git.Repo, cfg config.Config, changes []git.FileChange, untracked []string, styles Styles, t theme.Theme, stagedOnly bool, ref string) Model {
	files := buildFileItems(repo, changes, untracked)

	ti := textinput.New()
	ti.Placeholder = "commit message..."
	ti.CharLimit = 200

	bf := textinput.New()
	bf.Placeholder = "filter..."
	bf.CharLimit = 100
	bf.Width = fileListWidth - 8

	bi := textinput.New()
	bi.Placeholder = "branch name..."
	bi.CharLimit = 100

	// Resolving the target up front keeps the failure (missing clipboard
	// command, not inside tmux) attached to the send action rather than
	// blocking startup.
	target, targetErr := feedback.Resolve(feedback.Config{
		Target:     cfg.FeedbackTarget,
		TmuxTarget: cfg.TmuxTarget,
	})

	ca := textarea.New()
	ca.Placeholder = "review comment..."
	ca.ShowLineNumbers = false
	ca.SetHeight(commentEditorHeight)

	return Model{
		repo:         repo,
		cfg:          cfg,
		files:        files,
		styles:       styles,
		theme:        t,
		stagedOnly:   stagedOnly,
		ref:          ref,
		splitDiff:    cfg.SplitDiff,
		prevCurs:     -1,
		commitInput:  ti,
		branchFilter: bf,
		branchInput:  bi,
		commentInput: ca,
		target:       target,
		targetErr:    targetErr,
	}
}

func buildFileItems(repo *git.Repo, changes []git.FileChange, untracked []string) []fileItem {
	var files []fileItem
	for _, c := range changes {
		files = append(files, fileItem{change: c})
	}
	for _, path := range untracked {
		added := 0
		if repo != nil {
			raw, err := repo.ReadFileContent(path)
			if err == nil {
				added = countLines(raw)
			}
		}
		files = append(files, fileItem{change: git.FileChange{Path: path, Status: git.StatusUntracked, AddedLines: added}, untracked: true})
	}
	return files
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	count := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		count++
	}
	return count
}

func filesEqual(a, b []fileItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (m *Model) StartInCommitMode() {
	m.mode = modeCommit
	m.commitInput.Focus()
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadDiffCmd(true), m.fetchUpstreamStatusCmd(), tickCmd()}
	if m.mode == modeCommit {
		cmds = append(cmds, textinput.Blink)
	}
	return tea.Batch(cmds...)
}

// footerHeight is how many rows the footer takes, measured from what is
// actually rendered rather than assumed: the comment editor is a multiline
// textarea, the key hints wrap on narrow terminals, and the status may take a
// row of its own.
func (m Model) footerHeight() int {
	if m.width <= 0 {
		return 1
	}
	return lipgloss.Height(m.renderFooter())
}

// contentHeight is the room left for the panels.
func (m Model) contentHeight() int { return m.height - chromeRows - m.footerHeight() }

// listHeight is the rows a panel's list actually gets: the panel area less its
// label and the blank line under it. Scroll clamping and rendering must both
// use this, or the cursor can sit outside the visible window.
func (m Model) listHeight() int { return max(m.contentHeight()-2, 0) }
func (m Model) diffWidth() int  { return m.width - fileListWidth - 2 - 1 - 2 }
