package feedback

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Finding the agent to send a review to.
//
// tmux_target had to be written into the config by hand, and left unset it
// defaulted to the last active pane — right in a two-pane layout and wrong in
// every other. With several agents running there was no way to say which, and
// no way to see which differ would pick.
//
// Discovery is two processes for the whole scan, whatever the number of panes:
// one tmux listing and one ps listing.

// Agent is a tmux pane with a coding agent running in it.
type Agent struct {
	Pane    string // tmux pane id, e.g. "%12"
	Session string
	Window  string
	Tool    string
	Dir     string // the pane's working directory
}

// Label is how the agent reads in a picker.
func (a Agent) Label() string {
	return a.Session + ":" + a.Window
}

// agentTools are the commands that count as an agent.
//
// Matched on the executable's own name, so `vim claude.md` and
// `less codex.log` are not agents and neither is `claudette`. Add to this
// rather than loosening the match.
var agentTools = []string{"claude", "codex", "gemini", "copilot", "opencode", "aider"}

// aliases are the other names an agent's executable goes by.
//
// An npm-installed Claude Code is `@anthropic-ai/claude-code`, whose basename
// is claude-code, or `.../claude-code/cli.js`. The runner list was added
// because "Claude Code is often node .../claude" and then did not cover the
// shapes the npm package actually has; codex worked only because its scope is
// `@openai/codex`, so its basename happened to be the tool name.
var aliases = map[string]string{
	"claude-code": "claude",
	"gemini-cli":  "gemini",
	// aider's own documented invocation is `uvx --from aider-chat aider`,
	// and the package it comes from is aider-chat.
	"aider-chat": "aider",
}

// passThrough are subcommands and flags that still lead to the thing being
// run.
//
// `uv run aider`, `uv tool run aider`, `pipx run aider` and `poetry run
// aider` all do run the agent — "run" was in notRunning for `npm run
// <script>`, so every one of them was missed. The two are told apart by what
// follows: npm's script name is not an agent's name unless someone named a
// script after one, and `npm run aider` reaching aider is a better failure
// than `uv run aider` reaching nothing.
var passThrough = map[string]bool{
	"exec": true, "x": true, "tool": true,
	"--from": true, "-y": true,
}

// runMeansExecute names the runners whose `run` executes a package rather
// than a script defined in a manifest.
//
// `uv run aider`, `uv tool run aider`, `pipx run aider` and `poetry run
// aider` all start the agent, and "run" being unconditionally in notRunning
// missed every one. `npm run aider` is a script that happens to share the
// name and is not.
var runMeansExecute = map[string]bool{
	"uv": true, "uvx": true, "pipx": true, "poetry": true,
}

// runners are the commands that launch something else, and so may name an
// agent in their arguments rather than being one.
var runners = map[string]bool{
	"node": true, "npm": true, "npx": true, "bun": true, "bunx": true,
	"deno": true, "pnpm": true, "yarn": true, "poetry": true,
	"python": true, "python3": true, "uv": true, "uvx": true, "pipx": true,
	"sh": true, "bash": true, "zsh": true, "fish": true, "env": true,
	// gh copilot: the agent is the subcommand, not the executable.
	"gh": true,
}

// fieldSep separates the fields of one pane listing.
//
// A tab, because it is the one that works. US (0x1f) was tried, to stop a tab
// in a *session name* shifting every field after it — and on Linux tmux does
// not pass that byte through a format string, so every row came back
// unparseable and discovery found nothing at all. A theoretical problem
// traded for a real one; the tab is back and the limitation is stated instead.
//
// A tab in a session name therefore still shifts the fields, and parsePanes
// drops such a row rather than mislabelling it. A tab in the *path* is safe,
// because the path is the last field and is not split.
const fieldSep = "\t"

var paneFormat = strings.Join([]string{
	"#{pane_id}", "#{pane_pid}", "#{session_name}",
	"#{window_index}", "#{pane_current_path}",
}, fieldSep)

// Agents lists every pane on the tmux server running a coding agent, in the
// order the picker should offer them.
//
// The ordering is done here rather than by the caller: it was, and removing
// the caller's one line left an unsorted picker and the suite green.
func Agents(ctx context.Context, repoRoot string) ([]Agent, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil, fmt.Errorf("tmux is not installed — there is nothing to pick from")
	}
	panesOut, err := exec.CommandContext(ctx, "tmux", "list-panes", "-a", "-F", paneFormat).Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes: %w", err)
	}
	// One listing for the whole tree. Asking per pane would be a process per
	// pane, and there can be dozens.
	procsOut, err := exec.CommandContext(ctx, "ps", "-Ao", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil, err
	}
	self := os.Getenv("TMUX_PANE")
	panes := parsePanes(string(panesOut))
	return discover(panes, parseProcs(string(procsOut)),
		self, sessionOf(panes, self), repoRoot), nil
}

// sessionOf names the session a pane is in, from the listing already in hand.
//
// This used to be OwnSession, a third `tmux display-message`. The listing
// covers every pane on the server, differ's own included, so the answer was
// already there — and issue #84's "the whole scan is two subprocesses" was
// not met while it was being asked for separately.
func sessionOf(panes []pane, self string) string {
	if self == "" {
		return ""
	}
	for _, p := range panes {
		if p.id == self {
			return p.session
		}
	}
	return ""
}

// discover is everything Agents does once the two commands have answered:
// pair each pane with its agent, then order them. Pure, so the picker's whole
// input can be tested without a tmux server.
func discover(panes []pane, procs procTable, self, ownSession, repoRoot string) []Agent {
	found := findAgents(panes, procs, self)
	SortAgents(found, ownSession, repoRoot)
	return found
}

// pane is one row of the tmux listing.
type pane struct {
	id      string
	pid     int
	session string
	window  string
	dir     string
}

func parsePanes(out string) []pane {
	var panes []pane
	for _, line := range strings.Split(out, "\n") {
		// SplitN, so a separator inside the last field — the path — stays in
		// it rather than making a sixth. Exactly five: a short row is a row
		// whose fields do not mean what their positions say.
		f := strings.SplitN(strings.TrimRight(line, "\r"), fieldSep, 5)
		if len(f) != 5 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(f[1]))
		if err != nil {
			continue
		}
		// window_index is always a number, so a non-numeric one means the
		// fields have shifted — a tab inside a session name, which the
		// separator cannot distinguish. Dropping the row is right: a pane
		// labelled with someone else's window is worse than a pane missing
		// from the list.
		if _, err := strconv.Atoi(strings.TrimSpace(f[3])); err != nil {
			continue
		}
		panes = append(panes, pane{id: f[0], pid: pid, session: f[2], window: f[3], dir: f[4]})
	}
	return panes
}

// procTable is a process tree: children by parent, and each command.
type procTable struct {
	children map[int][]int
	command  map[int]string
}

func parseProcs(out string) procTable {
	t := procTable{children: map[int][]int{}, command: map[int]string{}}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		t.children[ppid] = append(t.children[ppid], pid)
		t.command[pid] = commandColumn(line)
	}
	return t
}

// commandColumn returns everything after the pid and ppid columns, kept whole:
// an argument can contain spaces and the executable is the first word.
//
// Cut by position, not by searching for the third field. strings.Index finds
// the *first* occurrence anywhere in the line, so a command that begins with
// digits already present in the pid sliced from the wrong place:
// "51234     1 1234 --foo" yielded the command "1234     1 1234 --foo", whose
// first word is then a number, so the process matched no agent and every
// child of it was lost with it.
func commandColumn(line string) string {
	rest := line
	for range 2 {
		rest = strings.TrimLeft(rest, " \t")
		i := strings.IndexAny(rest, " \t")
		if i < 0 {
			return ""
		}
		rest = rest[i:]
	}
	return strings.TrimSpace(rest)
}

// findAgents pairs each pane with the agent running under it, if any.
func findAgents(panes []pane, procs procTable, self string) []Agent {
	var found []Agent
	for _, p := range panes {
		// Never differ's own pane. tmuxTarget refuses to send there, and that
		// refusal is not one that reopens the picker — so offering it, and
		// sorting it first, was offering the one choice that can never work.
		if self != "" && p.id == self {
			continue
		}
		if tool := procs.toolUnder(p.pid); tool != "" {
			found = append(found, Agent{
				Pane: p.id, Session: p.session, Window: p.window,
				Tool: tool, Dir: p.dir,
			})
		}
	}
	return found
}

// oneShot reports whether a command line runs an agent non-interactively,
// so there is no session to paste a review into.
//
// `claude -p` is differ's own default commit_msg_cmd: a second differ
// generating a commit message made its pane look like an agent for those few
// seconds, and a review pasted there goes into a process that has already
// read its stdin. `--help`, `doctor` and the like are the same shape.
func oneShot(fields []string) bool {
	for _, arg := range fields[1:] {
		switch arg {
		case "-p", "--print", "--help", "-h", "--version", "doctor", "mcp":
			return true
		}
	}
	return false
}

// toolUnder walks the tree from pid and names the first agent it finds.
func (t procTable) toolUnder(pid int) string {
	seen := map[int]bool{}
	todo := []int{pid}
	for len(todo) > 0 {
		current := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if seen[current] {
			continue // a cycle would otherwise never end
		}
		seen[current] = true
		if tool := agentTool(t.command[current]); tool != "" {
			return tool
		}
		todo = append(todo, t.children[current]...)
	}
	return ""
}

// agentTool names the agent a command line runs, or "".
//
// The executable's own name, not a substring of the whole line: `vim claude.md`
// edits a file and `claudette` is something else.
func agentTool(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	if oneShot(fields) {
		return ""
	}
	exe := filepath.Base(strings.TrimPrefix(fields[0], "-"))
	if tool := toolNamed(exe); tool != "" {
		return tool
	}
	// A runner names the agent in its arguments: Claude Code is often
	// `node /usr/local/bin/claude`. Only runners get their arguments read —
	// scanning every command's arguments made `grep -r aider .` an agent.
	if !runners[exe] {
		return ""
	}
	// The *first* runnable argument, not any of them. Scanning all of them
	// made `env vim claude` an agent: env is a runner, vim is what it runs,
	// and claude is a file vim was opening.
	for _, arg := range fields[1:] {
		switch {
		case arg == "run":
			if runMeansExecute[exe] {
				continue
			}
			// npm and friends: a script from the manifest, which is not the
			// agent even when it shares its name.
			return ""
		case strings.HasPrefix(arg, "-") && !passThrough[arg], strings.Contains(arg, "="):
			continue // a flag, or env's KEY=VALUE
		case passThrough[arg]:
			continue // `npm exec <pkg>`
		}
		base := filepath.Base(arg)
		if tool := toolNamed(base); tool != "" {
			return tool
		}
		// `.../claude-code/cli.js` — the entry point is named after node, so
		// the directory holding it names the agent.
		if tool := toolNamed(filepath.Base(filepath.Dir(arg))); tool != "" {
			return tool
		}
		if runners[base] {
			continue // one runner invoking another
		}
		// Whatever this is, it is what the runner runs, and it is not an
		// agent. `uv pip install aider` stops at "pip" and never reaches
		// "aider"; so does `pipx install aider` at "install".
		//
		// There used to be a notRunning table of subcommands here as well —
		// install, add, upgrade, pip — and it was doing nothing: this return
		// already stops at the first token that is not a tool and not a
		// runner, which is every one of them, and deleting the whole branch
		// left the suite green. A table that looks like it is making a
		// decision and is not is worse than no table.
		return ""
	}
	return ""
}

// SortAgents puts the most likely one first: in differ's own tmux session,
// then working in the repository, then by window.
func SortAgents(agents []Agent, ownSession, repoRoot string) {
	score := func(a Agent) int {
		n := 0
		if ownSession != "" && a.Session == ownSession {
			n -= 2
		}
		if repoRoot != "" && underRoot(a.Dir, repoRoot) {
			n--
		}
		return n
	}
	sort.SliceStable(agents, func(i, j int) bool {
		if si, sj := score(agents[i]), score(agents[j]); si != sj {
			return si < sj
		}
		if agents[i].Session != agents[j].Session {
			return agents[i].Session < agents[j].Session
		}
		// By number, not by string: windows 1, 2 and 10 sort as 1, 10, 2 the
		// other way, and the issue asks for the index.
		return windowIndex(agents[i].Window) < windowIndex(agents[j].Window)
	})
}

func underRoot(dir, root string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// toolNamed maps an executable's own name to the agent it is, or "".
func toolNamed(name string) string {
	for _, tool := range agentTools {
		if name == tool {
			return tool
		}
	}
	return aliases[name]
}

// windowIndex reads a tmux window index, or a large number so that anything
// unparseable sorts last rather than first.
func windowIndex(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 1 << 30
	}
	return n
}
