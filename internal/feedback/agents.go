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

// runners are the commands that launch something else, and so may name an
// agent in their arguments rather than being one.
var runners = map[string]bool{
	"node": true, "npm": true, "npx": true, "bun": true, "bunx": true,
	"deno": true, "pnpm": true, "yarn": true,
	"python": true, "python3": true, "uv": true, "uvx": true, "pipx": true,
	"sh": true, "bash": true, "zsh": true, "fish": true, "env": true,
}

// paneFormat is tab-separated because a pane's path can contain spaces.
var paneFormat = "#{pane_id}\t#{pane_pid}\t#{session_name}\t#{window_index}\t#{pane_current_path}"

// Agents lists every pane on the tmux server running a coding agent.
func Agents(ctx context.Context) ([]Agent, error) {
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
	return findAgents(parsePanes(string(panesOut)), parseProcs(string(procsOut))), nil
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
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) < 5 {
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
		// The command is everything after the two numbers, kept whole: an
		// argument can contain spaces and the executable is the first word.
		t.command[pid] = strings.TrimSpace(line[strings.Index(line, fields[2]):])
	}
	return t
}

// findAgents pairs each pane with the agent running under it, if any.
func findAgents(panes []pane, procs procTable) []Agent {
	var found []Agent
	for _, p := range panes {
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
	for _, tool := range agentTools {
		if exe == tool {
			return tool
		}
	}
	// A runner names the agent in its arguments: Claude Code is often
	// `node /usr/local/bin/claude`. Only runners get their arguments read —
	// scanning every command's arguments made `grep -r aider .` an agent.
	if !runners[exe] {
		return ""
	}
	for _, arg := range fields[1:] {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		base := filepath.Base(arg)
		for _, tool := range agentTools {
			if base == tool {
				return tool
			}
		}
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
		return agents[i].Window < agents[j].Window
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
