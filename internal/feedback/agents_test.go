package feedback

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Finding the agent means walking each pane's process tree, not reading
// pane_current_command.
//
// An agent started through a wrapper — npx, a shell function, a script — does
// not appear as the pane's current command, so matching on that misses it.
// sidekick.nvim walks the tree from pane_pid for this reason; this is the same
// idea against one `ps` listing.
func TestFindAgents_WalksTheProcessTree(t *testing.T) {
	t.Parallel()
	// pane 1 runs claude directly; pane 2 runs it under a wrapper; pane 3 is a
	// shell with nothing in it.
	procs := parseProcs(strings.Join([]string{
		"  100     1 -zsh",
		"  101   100 node /usr/local/bin/claude",
		"  200     1 -zsh",
		"  201   200 npm exec opencode",
		"  202   201 /opt/homebrew/bin/opencode serve",
		"  300     1 -zsh",
		"  301   300 vim README.md",
	}, "\n"))

	panes := parsePanes(strings.Join([]string{
		"%1\t100\twork\t0\t/home/u/proj",
		"%2\t200\twork\t1\t/home/u/proj",
		"%3\t300\twork\t2\t/home/u/proj",
	}, "\n"))

	got := findAgents(panes, procs)
	if len(got) != 2 {
		t.Fatalf("found %d agents, want 2: %+v", len(got), got)
	}
	byPane := map[string]Agent{}
	for _, a := range got {
		byPane[a.Pane] = a
	}
	if a := byPane["%1"]; a.Tool != "claude" {
		t.Errorf("pane %%1 tool = %q, want claude", a.Tool)
	}
	if a := byPane["%2"]; a.Tool != "opencode" {
		t.Errorf("pane %%2 tool = %q, want opencode — a wrapped agent was missed", a.Tool)
	}
	if _, ok := byPane["%3"]; ok {
		t.Error("a pane running a shell and an editor was offered as an agent")
	}
}

// The tools the issue names, and nothing that merely contains their letters.
func TestAgentTool_RecognisesTheToolsAndNothingElse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ cmd, want string }{
		{"claude", "claude"},
		{"/opt/homebrew/bin/claude", "claude"},
		{"node /usr/local/bin/claude --resume", "claude"},
		{"codex", "codex"},
		{"gemini chat", "gemini"},
		{"copilot", "copilot"},
		{"opencode serve", "opencode"},
		{"aider --model gpt-4", "aider"},
		// Not agents, however much they look like one.
		{"vim claude.md", ""},
		{"less /var/log/codex.log", ""},
		{"grep -r aider .", ""},
		{"claudette", ""},
		{"-zsh", ""},
		{"", ""},
	} {
		if got := agentTool(tc.cmd); got != tc.want {
			t.Errorf("agentTool(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

// Only a runner's arguments are read. Scanning every command's arguments made
// `grep -r aider .` an agent.
func TestAgentTool_OnlyARunnerNamesAnAgentInItsArguments(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ cmd, want string }{
		// Runners, which really do launch agents this way.
		{"node /usr/local/bin/claude", "claude"},
		{"npm exec opencode", "opencode"},
		{"bun /opt/x/codex", "codex"},
		// python -m aider is a real way to run it, so this matches — the
		// module name after -m is the agent.
		{"python3 -m aider", "aider"},
		{"env CLAUDE_X=1 claude", "claude"},
		// Not runners, so their arguments are none of our business.
		{"grep -r aider .", ""},
		{"less /var/log/codex.log", ""},
		{"vim claude", ""},
		{"tail -f gemini", ""},
		{"rg opencode", ""},
	} {
		if got := agentTool(tc.cmd); got != tc.want {
			t.Errorf("agentTool(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

// The most likely agent comes first: the one in differ's own tmux session,
// then one working in this repository, then by window.
func TestSortAgents_PutsTheLikelyOneFirst(t *testing.T) {
	t.Parallel()
	agents := []Agent{
		{Pane: "%9", Session: "other", Window: "0", Dir: "/elsewhere"},
		{Pane: "%3", Session: "mine", Window: "3", Dir: "/elsewhere"},
		{Pane: "%2", Session: "mine", Window: "1", Dir: "/repo/sub"},
		{Pane: "%8", Session: "other", Window: "0", Dir: "/repo"},
	}
	SortAgents(agents, "mine", "/repo")

	var order []string
	for _, a := range agents {
		order = append(order, a.Pane)
	}
	want := []string{"%2", "%3", "%8", "%9"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// A directory beside the repository is not inside it. filepath.Rel gives
// "../repo-other" for that, which a prefix check on the name alone would
// accept.
func TestSortAgents_ASiblingDirectoryIsNotInsideTheRepo(t *testing.T) {
	t.Parallel()
	if underRoot("/home/u/repo-other", "/home/u/repo") {
		t.Error("a sibling directory counted as inside the repository")
	}
	if !underRoot("/home/u/repo/internal/ui", "/home/u/repo") {
		t.Error("a subdirectory did not count as inside the repository")
	}
	if !underRoot("/home/u/repo", "/home/u/repo") {
		t.Error("the root itself did not count")
	}
}

// Against a real tmux server. Skips without tmux, so it never runs in CI —
// ubuntu-latest has none — which means it only runs when someone runs it.
func TestAgents_FindsAnAgentInARealTmuxSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	// A pane whose *child* is the agent, which is the case matching on
	// pane_current_command would miss.
	name := "differ-test-agents-" + strconv.Itoa(os.Getpid())
	dir := t.TempDir()
	fake := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nread x\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	_ = exec.Command("tmux", "kill-session", "-t", name).Run()
	// sh -c keeps a shell as the pane's process and the fake agent as its
	// child, which is the shape this is about.
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name, "sh", "-c", fake+"; sleep 30")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not start a tmux session: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", name).Run() })
	time.Sleep(400 * time.Millisecond) // let the child appear in ps

	found, err := Agents(context.Background())
	if err != nil {
		t.Fatalf("Agents: %v", err)
	}
	for _, a := range found {
		if a.Session == name {
			if a.Tool != "claude" {
				t.Errorf("tool = %q, want claude", a.Tool)
			}
			if a.Pane == "" {
				t.Error("no pane id")
			}
			return
		}
	}
	t.Errorf("no agent found in session %q; got %+v", name, found)
}
