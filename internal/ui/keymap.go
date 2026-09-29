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
		{Keys: []string{"ctrl+c"}, Desc: "quit", Help: "quit immediately"},
	}
}

// keymapFor is every command available in a mode, in the order it should be
// offered.
func keymapFor(mode viewMode) []binding {
	switch mode {
	case modeDiff:
		return diffBindings(false)
	case modeReview:
		return diffBindings(true)
	case modeCommit:
		return []binding{
			{Keys: []string{"enter"}, Desc: "commit", Help: "commit the staged changes with this message", Bar: true},
			{Keys: []string{"esc"}, Desc: "cancel", Help: "discard the message and go back", Bar: true},
		}
	case modeBranchPicker:
		return []binding{
			// No key of its own: any printable character goes to the filter.
			{Label: "type", Desc: "filter", Help: "type to narrow the list", Bar: true},
			{Keys: []string{"enter"}, Desc: "switch", Help: "check out the selected branch", Bar: true, Confirm: true},
			{Keys: []string{"up", "ctrl+k"}, Label: "↑/^k", Desc: "up", Help: "move up the list", Bar: true},
			{Keys: []string{"down", "ctrl+j"}, Label: "↓/^j", Desc: "down", Help: "move down the list", Bar: true},
			{Keys: []string{"ctrl+n"}, Label: "^n", Desc: "new", Help: "create a branch from the current HEAD", Bar: true},
			{Keys: []string{"esc"}, Desc: "clear/close", Help: "clear the filter, or close the picker when it is empty", Bar: true},
		}
	default:
		return fileListBindings()
	}
}

func fileListBindings() []binding {
	return []binding{
		{Keys: []string{"j", "down"}, Label: "j/k", Desc: "navigate", Help: "move through the changed files", Bar: true},
		{Keys: []string{"k", "up"}},
		{Keys: []string{"enter", "l", "right"}, Label: "enter", Desc: "open", Help: "open this file's diff", Bar: true},
		{Keys: []string{"r"}, Desc: "review", Help: "open this file in review mode, where you can comment", Bar: true},
		{Keys: []string{"tab"}, Desc: "stage", Help: "stage or unstage this file", Bar: true},
		{Keys: []string{"a"}, Desc: "stage all", Help: "stage every change"},
		{Keys: []string{"c"}, Desc: "commit", Help: "write a commit message for the staged changes", Bar: true},
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

// diffBindings are the diff view's keys, plus review's own when reviewing.
//
// The two share navigation deliberately: a cursor position means the same
// thing in both, so the keys that move it must too.
func diffBindings(review bool) []binding {
	nav := []binding{
		{Keys: []string{"j", "down"}, Label: "j/k", Desc: "line", Help: "move the cursor a line", Bar: true},
		{Keys: []string{"k", "up"}},
		{Keys: []string{"}", "]"}, Label: "}/{", Desc: "hunk", Help: "jump to the next or previous hunk", Bar: true},
		{Keys: []string{"{", "["}},
		{Keys: []string{"d"}, Label: "d/u", Desc: "½ page", Help: "scroll half a page down or up"},
		{Keys: []string{"u"}},
		{Keys: []string{"n"}, Label: "n/p", Desc: "file", Help: "move to the next or previous file", Bar: true},
		{Keys: []string{"p"}},
		{Keys: []string{"g"}, Desc: "top", Help: "jump to the first line"},
		{Keys: []string{"G"}, Desc: "bottom", Help: "jump to the last line"},
	}

	var own []binding
	if review {
		own = []binding{
			{Keys: []string{"c"}, Desc: "comment", Help: "comment on the line under the cursor", Bar: true},
			{Keys: []string{"C"}, Desc: "hunk comment", Help: "comment on the whole hunk", Bar: true},
			{Keys: []string{"x"}, Desc: "delete", Help: "delete the comment under the cursor"},
			{Keys: []string{"s"}, Label: "s/S", Desc: "send", Help: "send the comment under the cursor, or S for all of them", Bar: true},
			{Keys: []string{"S"}},
			{Keys: []string{"r"}, Desc: "exit review", Help: "go back to the plain diff", Bar: true},
		}
	} else {
		own = []binding{
			{Keys: []string{"r"}, Desc: "review", Help: "start reviewing this file", Bar: true},
		}
	}

	tail := []binding{
		{Keys: []string{"e"}, Desc: "edit", Help: "open this file in your editor"},
		{Keys: []string{"tab"}, Desc: "stage", Help: "stage or unstage this file"},
		{Keys: []string{"v"}, Desc: "split", Help: "toggle the side-by-side diff"},
		{Keys: []string{"b"}, Desc: "branch", Help: "switch branches"},
		{Keys: []string{"q"}, Desc: "quit", Help: "quit differ", Bar: true},
	}
	if !review {
		tail = append([]binding{
			{Keys: []string{"esc", "h", "left"}, Label: "esc", Desc: "back", Help: "back to the file list", Bar: true},
		}, tail...)
	} else {
		tail = append([]binding{
			{Keys: []string{"esc"}, Desc: "back", Help: "back to the file list"},
		}, tail...)
	}

	return append(append(nav, own...), tail...)
}
