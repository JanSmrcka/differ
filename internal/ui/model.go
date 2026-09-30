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
	"github.com/jansmrcka/differ/internal/editor"
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

const (
	// The file list takes a share of the terminal rather than a fixed slice,
	// between these bounds: below minListWidth a path tells you nothing, and
	// above maxListWidth the extra columns are better spent on the diff.
	minListWidth = 24
	maxListWidth = 44
	// listShare is the fraction of the terminal the file list asks for.
	listShare = 4

	// twoPanelWidth is the narrowest terminal that fits both panels with the
	// diff still readable. Below it one panel takes the whole width — a
	// squeezed pair is worse than one of them.
	twoPanelWidth = 72
)

// How often the repository is probed. It used to be two seconds because each
// tick cost eight git processes; the probe costs one, so asking twice as often
// is still four times cheaper than what it replaced — and a change now shows up
// in about a second rather than two.
const pollInterval = 1 * time.Second

const (
	// The last resort. Below this there is not room for one usable panel, let
	// alone two, and saying so is better than drawing nonsense.
	minWidth  = 40
	minHeight = 8
)

type tickMsg time.Time

type diffLoadedMsg struct {
	// The file this diff was read for.
	//
	// index alone could not identify it: it indexes m.files, which the next
	// refresh replaces, so a load in flight when the changeset reordered was
	// installed against whatever had taken its slot. The panel then showed one
	// file's diff under another's name, drew that file's comments on it, and
	// re-anchored them against the wrong parse — marking valid comments stale.
	path string

	// The content key this diff was built from, read at the same moment as the
	// content itself rather than looked up afterwards.
	key string
	// True when this is the diff already on screen, rebuilt at a new width or
	// palette. Its content is by definition unchanged, so it must not
	// re-anchor comments: the parse it carries still contains the lines the
	// agent has since deleted, and re-anchoring against it restores every
	// comment the refresh had just marked stale.
	rerender bool

	// renderer is nil when the diff could not be loaded; errContent then holds
	// the message to show instead.
	renderer    *DiffRenderer
	errContent  string
	index       int
	resetScroll bool
	// themeGen is the theme this renderer was built under.
	themeGen int
}

// filesRefreshedMsg carries the current changeset. err is set when git could
// not be read — an empty file list then means "unknown", not "nothing
// changed", which matters because review comments are staled off this.
type filesRefreshedMsg struct {
	// The fingerprint the repository had when this refresh was asked for, and
	// a sequence number. The fingerprint is stored only when the refresh
	// lands, so a failed one is retried rather than assumed; the sequence
	// drops a refresh that is older than one already installed, which would
	// otherwise leave the screen behind with a fingerprint claiming it was
	// current — and no probe would ever correct it.
	fingerprint string
	seq         int

	files []fileItem
	// keys fingerprints each file's content, so a refresh can say which files
	// moved rather than only that the changeset did. The file list is
	// deliberately not keyed on it: a touch with no edit must not scroll the
	// reviewer back to the top of the diff.
	keys map[string]string
	err  error
}
type commitDoneMsg struct{ err error }

// reanchorMsg carries each commented file's current line positions, so
// comments on files that are not on screen can be re-resolved too.
type reanchorMsg struct {
	locations map[string][]review.Location
}

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

// repoProbedMsg carries the answer to "did anything move?".
type repoProbedMsg struct {
	fingerprint string
	err         error
}
type pushDoneMsg struct{ err error }
type pullDoneMsg struct{ err error }
type savePrefDoneMsg struct{ err error }

type branchCreatedMsg struct {
	name string
	err  error
}

// Model holds all UI state; behavior split across focused files.
type Model struct {
	repo *git.Repo
	cfg  config.Config
	// files is the changeset on show; fileKeys fingerprints each one's
	// content as of the last refresh, which is how the next refresh can tell
	// a rewritten file from an untouched one.
	files    []fileItem
	fileKeys map[string]string
	// The last probe's fingerprint. An equal one means the tick can stop
	// without asking git anything else.
	repoFingerprint string
	// Whether a probe is out, and for how many ticks. tea.Tick does not wait
	// for the previous one, and git can block rather than fail.
	probing     bool
	probeWaited int
	// Ticks since the last full rebuild, so a burst coalesces.
	ticksSinceRefresh int
	// Whether the diff on screen is older than the repository.
	//
	// Only set in review mode: everywhere else a refresh lands straight away,
	// which is what makes differ feel live. A reviewer is reading one diff
	// closely and may have a comment half-written against it, so the content
	// is held and they are told, rather than swapped and left to notice.
	// What the diff on screen was built from: the file's content key, and its
	// added/removed counts at that moment.
	//
	// Staleness is derived from these rather than stored as a flag. A flag has
	// to be cleared, and every path that failed to clear it — an empty
	// changeset, a failed load, leaving review mode — left the bar describing
	// something that was not on screen, sometimes permanently. These cannot
	// disagree with the renderer, because they are written where it is.
	rendererKey   string
	rendererAdded int
	rendererGone  int

	// The last refresh asked for, and the newest one installed.
	refreshSeq   int
	installedSeq int
	// fileOffset is the first file on screen. The list is taller than the
	// panel in any real agent changeset, so without it the files past the
	// panel height were unreachable.
	fileOffset int
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

	lastDiffContent string

	// renderer holds the diff currently on screen; diffCursor indexes into its
	// lines and is the anchor review comments will attach to.
	renderer *DiffRenderer
	// rendererPath is the file the renderer describes. Diffs load
	// asynchronously, so between switching files and the diff arriving the
	// renderer still belongs to the previous one, and anything derived from
	// it would be about the wrong file.
	rendererPath string
	diffCursor   int
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

	// showHelp draws the full keymap over the panels. It is drawn over them
	// rather than below, so opening it cannot change the layout's height.
	showHelp bool
	// showHistory draws the session's delivery history over the panels, the
	// same way.
	showHistory bool
	// showProblem draws the last failure in full, and problem is that
	// failure — kept so the tool's own words are reachable without being in
	// the status bar.
	showProblem bool
	problem     *problem
	// showThemes draws the theme picker. themeBefore is what was in use when
	// it opened, so cancelling can put it back.
	showThemes  bool
	themeCursor int
	themeBefore theme.Theme
	// themeGen counts theme changes. A diff loaded under an older theme is
	// dropped rather than installed: two git diffs can be in flight and they
	// do not finish in order, so cancelling a preview could otherwise leave
	// the screen painted in the theme that was cancelled.
	themeGen int

	upstream     git.UpstreamInfo
	pushConfirm  bool
	pullConfirm  bool
	quitConfirm  bool
	staleConfirm bool

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

	// editorEnv is the process environment `e` decides from. It is read once
	// here, at the edge, so the decision stays a pure function of its inputs
	// and a test can describe a situation instead of arranging one.
	editorEnv editor.Env

	// session holds review state — comments and per-file progress. It is
	// created on first entering review mode, or restored from store at
	// startup when a previous run left something behind.
	session *review.Session
	// store is where the review is kept between runs: one file in this
	// checkout's own git directory. Nil when there is nowhere to write it, in
	// which case the review lives for the session as it always did.
	store *review.Store
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
	bf.Width = minListWidth - 8

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

	// Read once, here, rather than on every frame from inside View.
	branch := ""
	if repo != nil {
		branch = repo.BranchName()
	}

	// Whatever the last run left behind, silently. There is no prompt: being
	// asked "restore 3 saved comments?" on every start is a question with one
	// answer, and the comments that do come back are only the ones whose file
	// is still what it was.
	store, session := openReviewStore(repo, stagedOnly)

	return Model{
		// Open, so a change arriving in the first seconds refreshes at once
		// rather than waiting for the rate limit to fill.
		ticksSinceRefresh: refreshEvery,
		currentBranch:     branch,

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
		store:        store,
		session:      session,
		target:       target,
		targetErr:    targetErr,
		editorEnv:    editor.NewEnv(),
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

// StartInReviewMode opens straight into review, for `differ review`.
//
// It enters review mode even with nothing to review, so the panel can say so
// in the words the user asked for it in. Refusing to enter and putting
// "nothing to review" in the status bar meant the panel said "No changes" and
// the bar said something else — two answers to the same question.
func (m *Model) StartInReviewMode() {
	m.mode = modeReview
	if m.session == nil {
		// Not unconditionally: a session restored from the last run is
		// already here, and replacing it would throw away the comments that
		// were just read back.
		m.session = review.NewSession()
	}
	if len(m.files) > 0 {
		m.session.MarkViewed(m.currentFilePath())
	}
}

func (m *Model) StartInCommitMode() {
	m.mode = modeCommit
	m.commitInput.Focus()
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadDiffCmd(true), m.fetchUpstreamStatusCmd(), tickCmd()}
	// Restored comments have to be re-resolved against the diff as it is now.
	// The file on screen is re-anchored by its own load; every other one is
	// only reached from here, and a comment left on a line number nobody
	// checked would be delivered quoting the wrong place. Nothing to do when
	// there are no comments: reanchorAllCmd returns nil.
	if m.session != nil {
		cmds = append(cmds, m.reanchorAllCmd())
	}
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

// footerBudget is the most rows the footer may take.
//
// The frame is a header, two rules, the content and the footer. Whatever the
// footer would like, it never gets so much that no content row is left: the
// panels are what the user came for, and a frame taller than the terminal
// scrolls its own header off the screen.
func (m Model) footerBudget() int { return max(m.height-chromeRows-1, 1) }

// contentHeight is the room left for the panels.
//
// Never negative: View builds a slice of this length, and a footer that
// outgrew the terminal used to make that a panic rather than a squeeze.
func (m Model) contentHeight() int {
	return max(m.height-chromeRows-min(m.footerHeight(), m.footerBudget()), 0)
}

// listHeight is the rows a panel's list actually gets: the panel area less its
// label and the blank line under it. Scroll clamping and rendering must both
// use this, or the cursor can sit outside the visible window.
func (m Model) listHeight() int { return max(m.contentHeight()-2, 0) }

// listWidth is what the file list actually gets.
//
// A fixed 35 columns meant a narrow tmux split spent a third of itself on file
// names and left the diff — the thing differ is for — with whatever remained.
// It is a share of the terminal now, bounded at both ends, and the whole width
// when the layout has collapsed to one panel.
func (m Model) listWidth() int {
	// Deliberately not mode-dependent. In a collapsed layout both panels are
	// conceptually full width and only one is drawn — showsFileList decides
	// which. Sizing them by the mode instead meant a resize taken in the file
	// list built the diff viewport at width 0, and enter then opened a panel
	// with no room to render anything.
	if m.onePanel() {
		return m.width
	}
	return min(max(m.width/listShare, minListWidth), maxListWidth)
}

// onePanel reports whether only one panel is drawn.
//
// Either because the terminal is too narrow for both, or because there is no
// diff to put beside the list: with a clean tree the right-hand panel has
// nothing in it, and the empty state was being cut to a 24-column list while
// the empty panel next to it stayed blank.
func (m Model) onePanel() bool { return m.width < twoPanelWidth || len(m.files) == 0 }

// showsFileList reports which panel a collapsed layout keeps: the one the user
// is working in — or the file list regardless when there is no diff to show,
// since that is where the empty state explains why.
func (m Model) showsFileList() bool {
	if len(m.files) == 0 {
		return true
	}
	return m.mode == modeFileList || m.mode == modeBranchPicker
}

// diffWidth is what the right-hand panel gets: the terminal less the file
// list, the divider and the space either side of it.
func (m Model) diffWidth() int {
	if m.onePanel() {
		return m.width
	}
	return m.width - m.listWidth() - verticalDividerWidth - 2*panelGap
}
