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
// Discovery is three processes for the whole scan, whatever the number of panes
// (two outside tmux, where there is no own session to ask about):
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
}

// passThrough are subcommands that still lead to the thing being run.
var passThrough = map[string]bool{"exec": true, "x": true}

// notRunning are the subcommands that do something *to* a package rather than
// run it. Installing an agent is not running one, and `npm run aider` runs a
// script that happens to share the name.
var notRunning = map[string]bool{
	"install": true, "add": true, "remove": true, "uninstall": true,
	"update": true, "upgrade": true, "run": true, "pip": true, "test": true,
}

// runners are the commands that launch something else, and so may name an
// agent in their arguments rather than being one.
var runners = map[string]bool{
	"node": true, "npm": true, "npx": true, "bun": true, "bunx": true,
	"deno": true, "pnpm": true, "yarn": true,
	"python": true, "python3": true, "uv": true, "uvx": true, "pipx": true,
	"sh": true, "bash": true, "zsh": true, "fish": true, "env": true,
}

// fieldSep separates the fields of one pane listing.
//
// A tab was the obvious choice and the wrong one: a path may contain spaces,
// but a tmux *session name* may contain a tab, and one there shifted every
// field after it — `ses<TAB>name` parsed as session `ses`, window `name` and
// pid where the directory should be. US (0x1f) is a separator no shell will
// put in a name, and tmux passes it through a format string verbatim.
const fieldSep = "\x1f"

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
	return discover(
		parsePanes(string(panesOut)), parseProcs(string(procsOut)),
		os.Getenv("TMUX_PANE"), OwnSession(ctx), repoRoot,
	), nil
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
		case strings.HasPrefix(arg, "-"), strings.Contains(arg, "="):
			continue // a flag, or env's KEY=VALUE
		case notRunning[arg]:
			// This command line is about a package, not a process:
			// `pip install aider`, `npm run aider`.
			return ""
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
		// agent.
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

// OwnSession names the tmux session differ is running in, or "".
//
// Read from $TMUX_PANE rather than assumed, and empty outside tmux — and
// empty inside a display-popup too, where tmux does not set it. An empty
// answer only costs the ordering its first preference.
func OwnSession(ctx context.Context) string {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return ""
	}
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", pane, "#{session_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
