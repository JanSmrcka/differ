package ui

// The keymap: one table, read by the command bar, the help overlay and the
// tests that check it against the handlers.
//
// It used to be a list of hints written out per mode in render_layout.go,
// separate from the switch statements that actually handle keys and from the
// README table — three copies, any of which could drift. keymap_test.go now
// parses the handlers out of the source and fails the build if the table and
// the code disagree in either direction.

// binding is one command in one mode.
type binding struct {
	// Keys are every key that triggers it. The first is the canonical one,
	// the rest are aliases (arrows, vim equivalents).
	Keys []string
	// Label is how the keys are written to the user: "j/k" rather than the
	// full list of aliases.
	Label string
	// Desc is the short form for the command bar.
	Desc string
	// Help is the fuller sentence for the overlay. Empty falls back to Desc.
	Help string
	// Bar puts it in the one-line command bar. Everything appears in the
	// overlay regardless.
	Bar bool
	// Confirm marks a command that asks for a second press because it
	// changes something outside differ.
	Confirm bool
}

// label is how the binding is written in the UI.
func (b binding) label() string {
	if b.Label != "" {
		return b.Label
	}
	if len(b.Keys) == 0 {
		return ""
	}
	return b.Keys[0]
}

// help is the overlay text.
func (b binding) help() string {
	if b.Help != "" {
		return b.Help
	}
	return b.Desc
}

// globalBindings are the keys every mode answers, listed once.
func globalBindings() []binding {
	return []binding{
		{Keys: []string{"?"}, Desc: "help", Help: "show every key for this view", Bar: true},
		{Keys: []string{"H"}, Desc: "history", Help: "what has been sent, and whether it arrived"},
		{Keys: []string{"!"}, Desc: "problem", Help: "show the last failure in full, including what the tool said"},
		{Keys: []string{"t"}, Desc: "theme", Help: "try the themes; the screen changes as you move"},
		{Keys: []string{agentKey}, Desc: "agent", Help: "choose which agent in tmux the review is sent to"},
		{Keys: []string{"ctrl+c"}, Desc: "quit", Help: "quit immediately"},
	}
}

// keymapFor is every command available in a mode, in the order it should be
// offered.
func keymapFor(mode viewMode) []binding {
	switch mode {
	case modeDiff:
		return diffBindings()
	case modeCommit:
		return []binding{
			{Keys: []string{"enter"}, Desc: "commit", Help: "commit the staged changes with this message", Bar: true},
			{Keys: []string{"esc"}, Desc: "cancel", Help: "discard the message and go back", Bar: true},
		}
	case modeBranchPicker:
		return []binding{
			// No key of its own: any printable character goes to the filter.
			{Label: "type", Desc: "filter", Help: "type to narrow the list", Bar: true},
			// Not marked Confirm, because it asks only sometimes: git refuses
			// a switch that would overwrite local changes, but carries ones
			// that do not conflict across without a word — so with tracked
			// changes in the tree it takes a second enter on the same branch.
			{Keys: []string{"enter"}, Desc: "switch", Help: "check out the selected branch; asks again if uncommitted changes would move with you", Bar: true},
			{Keys: []string{"up", "ctrl+k"}, Label: "↑/^k", Desc: "up", Help: "move up the list", Bar: true},
			{Keys: []string{"down", "ctrl+j"}, Label: "↓/^j", Desc: "down", Help: "move down the list", Bar: true},
			{Keys: []string{"ctrl+n"}, Label: "^n", Desc: "new", Help: "create a branch from the current HEAD", Bar: true},
			{Keys: []string{"esc"}, Desc: "clear/close", Help: "clear the filter, or close the picker when it is empty", Bar: true},
		}
	default:
		return fileListBindings()
	}
}

// surface is something with keys of its own that is not a mode: an overlay,
// an input inside a mode, or the log browser. Each owns the keyboard while it
// is up, so each has a table here and keymap_test.go holds it to its handler.
type surface string

const (
	surfaceThemes    surface = "theme picker"
	surfaceAgents    surface = "agent picker"
	surfaceReading   surface = "help, history and problem"
	surfaceNewBranch surface = "new branch"
	surfaceComment   surface = "comment editor"
	surfaceLogList   surface = "log"
	surfaceLogDiff   surface = "log diff"
)

var allSurfaces = []surface{
	surfaceThemes, surfaceAgents, surfaceReading, surfaceNewBranch,
	surfaceComment, surfaceLogList, surfaceLogDiff,
}

// surfaceKeymap is every key a surface answers.
func surfaceKeymap(s surface) []binding {
	switch s {
	case surfaceThemes:
		return []binding{
			{Keys: []string{"j", "down"}, Label: "j/k", Desc: "move", Help: "try the next or previous theme", Bar: true},
			{Keys: []string{"k", "up"}},
			{Keys: []string{"enter"}, Desc: "keep", Help: "keep this theme and save it", Bar: true},
			{Keys: []string{"esc", "q", "t"}, Label: "esc", Desc: "cancel", Help: "go back to the theme you had", Bar: true},
		}
	case surfaceAgents:
		return []binding{
			{Keys: []string{"j", "down"}, Label: "j/k", Desc: "move", Help: "move through the agents", Bar: true},
			{Keys: []string{"k", "up"}},
			{Keys: []string{"enter"}, Desc: "choose", Help: "send reviews to this agent", Bar: true},
			{Keys: []string{"esc", "q", agentKey}, Label: "esc", Desc: "cancel", Help: "keep the agent you had", Bar: true},
		}
	case surfaceReading:
		return []binding{
			{Keys: []string{"?"}, Desc: "help", Help: "switch to the keys, or close them"},
			{Keys: []string{"H"}, Desc: "history", Help: "switch to what was sent, or close it"},
			{Keys: []string{"!"}, Desc: "problem", Help: "switch to the last failure, or close it"},
			{Keys: []string{"t"}, Desc: "theme", Help: "open the theme picker instead"},
			{Keys: []string{agentKey}, Desc: "agent", Help: "open the agent picker instead"},
			{Keys: []string{"esc", "q"}, Label: "esc", Desc: "close", Help: "close it", Bar: true},
		}
	case surfaceNewBranch:
		return []binding{
			{Keys: []string{"enter"}, Desc: "create", Help: "create the branch and switch to it", Bar: true},
			{Keys: []string{"esc"}, Desc: "cancel", Help: "back to the branch list", Bar: true},
		}
	case surfaceComment:
		return []binding{
			{Keys: []string{"ctrl+s"}, Desc: "save", Help: "save the comment", Bar: true},
			{Keys: []string{"esc"}, Desc: "cancel", Help: "discard what you typed", Bar: true},
		}
	case surfaceLogList:
		return []binding{
			{Keys: []string{"j", "down"}, Label: "j/k", Desc: "navigate", Help: "move through the commits", Bar: true},
			{Keys: []string{"k", "up"}},
			{Keys: []string{"g"}, Desc: "first", Help: "jump to the newest commit"},
			{Keys: []string{"G"}, Desc: "last", Help: "jump to the oldest commit shown"},
			{Keys: []string{"enter"}, Desc: "view diff", Help: "show this commit's diff", Bar: true},
			{Keys: []string{"q", "ctrl+c"}, Label: "q", Desc: "quit", Help: "quit", Bar: true},
		}
	case surfaceLogDiff:
		return []binding{
			// The viewport's own keys: no handler case of their own.
			{Label: "j/k", Desc: "scroll", Help: "scroll a line", Bar: true},
			{Label: "d/u", Desc: "½ page", Help: "scroll half a page", Bar: true},
			{Keys: []string{"esc"}, Desc: "back", Help: "back to the commit list", Bar: true},
			{Keys: []string{"q", "ctrl+c"}, Label: "q", Desc: "quit", Help: "quit", Bar: true},
		}
	}
	return nil
}

func fileListBindings() []binding {
	return []binding{
		{Keys: []string{"j", "down"}, Label: "j/k", Desc: "navigate", Help: "move through the changed files", Bar: true},
		{Keys: []string{"k", "up"}},
		{Keys: []string{"enter", "l", "right"}, Label: "enter", Desc: "open", Help: "open this file's diff", Bar: true},
		{Keys: []string{"tab"}, Desc: "stage", Help: "stage or unstage this file", Bar: true},
		{Keys: []string{"a"}, Desc: "stage all", Help: "stage every change"},
		{Keys: []string{"C"}, Desc: "commit", Help: "write a commit message for the staged changes", Bar: true},
		{Keys: []string{"e"}, Desc: "edit", Help: "open this file in your editor"},
		{Keys: []string{"b"}, Desc: "branch", Help: "switch branches"},
		{Keys: []string{"v"}, Desc: "split", Help: "toggle the side-by-side diff"},
		{Keys: []string{"g"}, Desc: "first", Help: "jump to the first file"},
		{Keys: []string{"G"}, Desc: "last", Help: "jump to the last file"},
		{Keys: []string{"P"}, Desc: "push", Help: "push to the upstream branch", Confirm: true},
		{Keys: []string{"F"}, Desc: "pull", Help: "pull, fast-forward only", Confirm: true},
		{Keys: []string{"q"}, Desc: "quit", Help: "quit differ", Bar: true},
	}
}

// diffBindings are the diff view's keys: reading and reviewing are one view.
func diffBindings() []binding {
	return []binding{
		{Keys: []string{"j", "down"}, Label: "j/k", Desc: "line", Help: "move the cursor a line", Bar: true},
		{Keys: []string{"k", "up"}},
		{Keys: []string{"}", "]"}, Label: "}/{", Desc: "hunk", Help: "jump to the next or previous hunk", Bar: true},
		{Keys: []string{"{", "["}},
		{Keys: []string{"d"}, Label: "d/u", Desc: "½ page", Help: "scroll half a page down or up"},
		{Keys: []string{"u"}},
		{Keys: []string{"J"}, Label: "J/K", Desc: "file", Help: "move to the next or previous file", Bar: true},
		{Keys: []string{"K"}},
		{Keys: []string{"g"}, Desc: "top", Help: "jump to the first line"},
		{Keys: []string{"G"}, Desc: "bottom", Help: "jump to the last line"},
		{Keys: []string{"c"}, Desc: "comment", Help: "comment on the line under the cursor", Bar: true},
		{Keys: []string{"C"}, Desc: "hunk comment", Help: "comment on the whole hunk"},
		{Keys: []string{"x"}, Desc: "delete", Help: "delete the comment under the cursor"},
		// Not marked Confirm, because it asks only sometimes: a stale comment
		// describes code that has moved, so sending one takes a second press.
		{Keys: []string{"s"}, Label: "s/S", Desc: "send", Help: "send the comment under the cursor, or S for all of them; a stale one asks again", Bar: true},
		{Keys: []string{"S"}},
		{Keys: []string{"R"}, Desc: "reload", Help: "re-read this file after it changed underneath you"},
		{Keys: []string{"esc", "h", "left"}, Label: "esc", Desc: "back", Help: "back to the file list", Bar: true},
		{Keys: []string{"e"}, Desc: "edit", Help: "open this file in your editor"},
		{Keys: []string{"tab"}, Desc: "stage", Help: "stage or unstage this file"},
		{Keys: []string{"v"}, Desc: "split", Help: "toggle the side-by-side diff"},
		{Keys: []string{"b"}, Desc: "branch", Help: "switch branches"},
		{Keys: []string{"q"}, Desc: "quit", Help: "quit differ", Bar: true},
	}
}
