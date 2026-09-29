package ui

import (
	"fmt"
	"strings"
)

// How a failure is presented.
//
// Every failure used to be git's stderr concatenated into the status bar —
// three lines of somebody else's voice in a one-line bar, saying nothing about
// what to do. The tools differ shells out to are entitled to be verbose; the
// status bar is not the place for it.
//
// Nothing here decides *whether* something failed. It decides how to say so,
// and it never discards the original text: for the failures nobody anticipated
// that text is the only useful thing there is.

// problem is a failure the user can see: what differ was doing, what to do
// about it, and the tool's own words kept aside until asked for.
type problem struct {
	// summary names the action, in differ's voice: "commit failed".
	summary string
	// hint is what to do about it, in one clause.
	hint string
	// detail is what the tool actually said, whole and unedited.
	detail string
}

// line is the one-line form for the status bar.
//
// The affordance comes before the hint on purpose. The bar is one row and cuts
// whatever does not fit, silently — so with the hint first, any failure whose
// hint ran long lost the "!" and the user was never told the detail existed,
// which is exactly when they needed it most.
func (p problem) line() string {
	out := p.summary
	if p.detail != "" {
		out += "  ·  ! details"
	}
	if p.hint != "" {
		out += "  ·  " + p.hint
	}
	return out
}

// remedy pairs a fragment of a tool's output with what to do about it.
//
// Matching on text is unavoidable: git has no error codes, and its wording is
// the only thing there is to go on. The fragments are the stable parts of each
// message — the bits that have not changed in years — and an unmatched failure
// still gets presented, so a reworded message degrades to "no hint" rather
// than to something wrong.
type remedy struct {
	fragment string
	hint     string
}

var remedies = []remedy{
	{"index.lock", "another git process is holding the index — try again in a moment"},
	{"not a git repository", "differ has to run inside a git repository"},
	{"unknown revision", "no branch, tag or commit by that name"},
	{"ambiguous argument", "no branch, tag or commit by that name"},
	{"nothing to commit", "nothing is staged — stage something with tab or a"},
	{"no changes added to commit", "nothing is staged — stage something with tab or a"},
	{"already exists", "a branch by that name already exists"},
	// Push and pull. "no upstream branch" comes from a plain push, where
	// suggesting P would be circular — the user just pressed it.
	{"no upstream branch", "this branch has no upstream — P offers to set one"},
	{"no tracking information", "push with P first to set the upstream"},
	{"non-fast-forward", "the remote has commits you do not — pull with F first"},
	{"fetch first", "the remote has commits you do not — pull with F first"},
	{"failed to push some refs", "the push was rejected — pull with F first"},
	{"not possible to fast-forward", "the branch has diverged — differ only pulls fast-forward"},
	{"your local changes", "commit or stash your changes first"},
	{"would be overwritten", "commit or stash your changes first"},
	{"fix conflicts", "resolve the conflict, then try again"},
	{"conflict (", "resolve the conflict, then try again"},
	// The remote refusing, in each of the three shapes it comes in. These are
	// before the generic permission fragment so a bare file-permission error
	// does not get told to check its credentials.
	{"could not read from remote", "check your access to the remote"},
	{"permission denied (publickey", "check your access to the remote"},
	{"permission to ", "check your access to the remote"},
	{"authentication failed", "check your access to the remote"},
	{"permission denied", "check the permissions on that path"},
	// The tools other than git.
	{"can't find pane", "the tmux target is gone — check tmux_target"},
	{"can't find session", "the tmux target is gone — check tmux_target"},
	{"no server running", "tmux is not running — check feedback_target"},
	{"executable file not found", "the command is not on PATH"},
}

// describe turns a failure into something worth reading.
//
// action is what differ was doing, in the imperative differ's own messages use
// — "commit", "push", "switch" — so the summary reads as "commit failed".
func describe(action string, err error) problem {
	if err == nil {
		return problem{}
	}
	raw := strings.TrimSpace(err.Error())
	p := problem{summary: action + " failed", detail: raw}

	lower := strings.ToLower(raw)
	for _, r := range remedies {
		if strings.Contains(lower, r.fragment) {
			p.hint = r.hint
			return p
		}
	}

	// Nothing recognised. The first line is usually the sentence that matters,
	// and the rest is kept for the details view.
	//
	// "usually": git push opens with "To <url>", which is never the sentence
	// that matters, so a bare remote path became the hint. That line is
	// skipped, and what is left is capped — an uncapped hint pushed the "!"
	// off the end of the bar.
	if first := firstUsefulLine(raw); first != "" {
		p.hint = truncateEnd(trimGitPrefix(first), maxHintWidth)
	}
	return p
}

// maxHintWidth keeps a fallback hint from crowding out the rest of the bar.
const maxHintWidth = 70

// firstUsefulLine is the first line that says something, skipping git push's
// "To <url>" banner.
func firstUsefulLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "To ") {
			continue
		}
		return line
	}
	return firstLine(s)
}

// firstLine is the first non-empty line of a tool's output.
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// trimGitPrefix drops git's own severity labels, which differ's summary
// already carries.
func trimGitPrefix(s string) string {
	for _, prefix := range []string{"fatal: ", "error: ", "warning: "} {
		s = strings.TrimPrefix(s, prefix)
	}
	return s
}

// fail records a failure and puts its one-line form in the status bar.
//
// It touches nothing else. A failure must never cost the user their place in
// the diff or a comment they have written — recovering from one is a matter of
// reading a line and trying again.
func (m Model) fail(action string, err error) Model {
	p := describe(action, err)
	m.problem = &p
	m.statusMsg = p.line()
	return m
}

// renderProblemOverlay shows the failure in full, including what the tool
// actually said.
func (m Model) renderProblemOverlay(width, height int) string {
	if m.problem == nil {
		return m.fitOverlay(" last problem", []string{
			m.styles.HelpDesc.Render(" nothing has gone wrong yet"),
		}, "! or esc to close", width, height)
	}

	rows := []string{m.styles.CommentStale.Render(" " + m.problem.summary)}
	if m.problem.hint != "" {
		rows = append(rows, " "+m.styles.HelpDesc.Render(m.problem.hint))
	}
	if m.problem.detail != "" {
		rows = append(rows, "", " "+m.styles.HelpKey.Render("what it said"))
		for line := range strings.SplitSeq(m.problem.detail, "\n") {
			rows = append(rows, " "+m.styles.HelpDesc.Render(strings.TrimRight(line, " ")))
		}
	}
	return m.fitOverlay(" last problem", rows, "! or esc to close", width, height)
}

// emptyState is what a panel says when there is nothing in it.
//
// A blank panel reads as a bug. These say what is true and, where there is
// one, what to do about it.
func (m Model) emptyState() []string {
	switch {
	case m.ref != "":
		return []string{"No differences", fmt.Sprintf("Nothing differs from %s.", m.ref)}
	case m.stagedOnly:
		return []string{"Nothing staged", "Stage something, or run differ without -s."}
	case m.mode == modeReview:
		// Reached two ways: opening `differ review` on a clean tree, and the
		// changeset emptying while a review is open. "yet" would be wrong for
		// the second — everything was just committed.
		return []string{"Nothing to review", "No changes to review."}
	default:
		return []string{"No changes", "Your working tree is clean."}
	}
}

// renderEmptyState draws the empty state into the file list's panel.
func (m Model) renderEmptyState() string {
	lines := m.emptyState()
	var rows []string
	// The blank lines are breathing room, and the first thing to go: at the
	// smallest terminal with a status message the panel is five rows, and the
	// padding used to push the explanation off the bottom.
	if m.listHeight() > len(lines)+2 {
		rows = append(rows, "")
	}
	rows = append(rows, " "+m.styles.PanelLabelFocus.Render(lines[0]))
	if m.listHeight() > len(lines) {
		rows = append(rows, "")
	}
	for _, l := range lines[1:] {
		rows = append(rows, " "+m.styles.HelpDesc.Render(l))
	}
	for i, r := range rows {
		rows[i] = padTo(r, fileListWidth)
	}
	return strings.Join(rows, "\n")
}
