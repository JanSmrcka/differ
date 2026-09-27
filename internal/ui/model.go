package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jansmrcka/differ/internal/config"
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

func (m Model) contentHeight() int { return m.height - 4 }
func (m Model) diffWidth() int     { return m.width - fileListWidth - 2 - 1 - 2 }
