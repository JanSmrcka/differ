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
		"%1\x1f100\x1fwork\x1f0\x1f/home/u/proj",
		"%2\x1f200\x1fwork\x1f1\x1f/home/u/proj",
		"%3\x1f300\x1fwork\x1f2\x1f/home/u/proj",
	}, "\n"))

	got := findAgents(panes, procs, "")
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
	// Blocks forever, so the pane stays alive without a `; sleep 30` after
	// it. That trailing sleep was read back from ps as part of one command
	// line, and strings.Fields then yielded "claude;" with the semicolon
	// attached — so whether this test passed depended on the *child*
	// process being matched instead, which is not a thing to depend on. It
	// held on macOS and not on ubuntu.
	script := "#!/bin/sh\nread x\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// Its own tmux server. On the shared one this test's session is visible
	// to internal/editor's tmux tests, which run in a parallel package and
	// look for a window running an editor — the two packages fought over one
	// server and failed each other intermittently.
	t.Setenv("TMUX_TMPDIR", shortTmuxDir(t))
	t.Setenv("TMUX", "")

	_ = exec.Command("tmux", "kill-session", "-t", name).Run()
	// sh -c keeps a shell as the pane's process and the fake agent as its
	// child, which is the shape this is about.
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name, "sh", "-c", fake)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not start a tmux session: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-server").Run() })
	time.Sleep(400 * time.Millisecond) // let the child appear in ps

	found, err := Agents(context.Background(), "")
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

// The shapes an npm-installed agent actually has.
//
// The runner list exists because Claude Code is often `node .../claude` — but
// the npm package's real forms are `@anthropic-ai/claude-code`, whose basename
// is claude-code, and `.../claude-code/cli.js`. Both were missed. codex worked
// only by the accident that its scope is `@openai/codex`, so the basename
// happened to be the tool name.
func TestAgentTool_FindsTheNpmInstalledShapes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ cmd, want string }{
		{"npx @anthropic-ai/claude-code", "claude"},
		{"npx -y @anthropic-ai/claude-code", "claude"},
		{"node /usr/local/lib/node_modules/@anthropic-ai/claude-code/cli.js", "claude"},
		{"npm exec @anthropic-ai/claude-code", "claude"},
		{"npx @openai/codex", "codex"},
		{"claude-code", "claude"},
		// Still not agents.
		{"vim claude-code.md", ""},
		{"grep -r claude-code .", ""},
		{"npm install @anthropic-ai/claude-code", ""},
	} {
		if got := agentTool(tc.cmd); got != tc.want {
			t.Errorf("agentTool(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

// A runner's arguments are read, but not its subcommands: installing an agent
// is not running one.
func TestAgentTool_InstallingAnAgentIsNotRunningOne(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{
		"python3 -m pip install aider",
		"uv pip install aider",
		"npm run aider",
		"npm install -g @anthropic-ai/claude-code",
		"yarn add opencode",
		"env vim claude",
	} {
		if got := agentTool(cmd); got != "" {
			t.Errorf("agentTool(%q) = %q, want none", cmd, got)
		}
	}
}

// differ's own pane is never offered. tmuxTarget refuses to send there, and
// that refusal is not one that reopens the picker — so offering it, and
// sorting it first, was offering the one choice that can never work.
func TestFindAgents_NeverOffersDiffersOwnPane(t *testing.T) {
	t.Parallel()
	procs := parseProcs("  100     1 claude\n  200     1 claude\n")
	panes := parsePanes("%1\x1f100\x1fwork\x1f0\x1f/repo\n%2\x1f200\x1fwork\x1f1\x1f/repo\n")

	got := findAgents(panes, procs, "%1")
	if len(got) != 1 {
		t.Fatalf("found %d agents, want 1: %+v", len(got), got)
	}
	if got[0].Pane == "%1" {
		t.Error("differ's own pane was offered")
	}
}

// Windows sort by index. As strings, 1, 2 and 10 come out 1, 10, 2.
func TestSortAgents_OrdersWindowsByNumber(t *testing.T) {
	t.Parallel()
	agents := []Agent{
		{Pane: "%c", Session: "s", Window: "10"},
		{Pane: "%a", Session: "s", Window: "2"},
		{Pane: "%b", Session: "s", Window: "1"},
	}
	SortAgents(agents, "", "")
	var order []string
	for _, a := range agents {
		order = append(order, a.Window)
	}
	for i, want := range []string{"1", "2", "10"} {
		if order[i] != want {
			t.Fatalf("window order = %v, want [1 2 10]", order)
		}
	}
}

// The npm-installed Claude Code is the shape the runner list exists for, and
// it was the shape the runner list missed: review round 1 found
// `npx @anthropic-ai/claude-code` and `node .../claude-code/cli.js` both
// naming nothing, while `npx -y @openai/codex` worked only because that
// package's basename happens to be the tool's name.
func TestAgentTool_FindsTheNpmShapesOfEachAgent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ command, want string }{
		{"npx @anthropic-ai/claude-code", "claude"},
		{"node /usr/local/lib/node_modules/@anthropic-ai/claude-code/cli.js", "claude"},
		{"npx -y @openai/codex", "codex"},
		{"node /usr/local/bin/claude", "claude"},
		{"-zsh", ""},
		{"python3 -m pip install aider", ""},
		{"npm run aider", ""},
		{"env vim claude", ""},
	} {
		if got := agentTool(tc.command); got != tc.want {
			t.Errorf("agentTool(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

// A login shell is `-zsh`, and stripping that leading dash is the whole
// reason a pane's own shell is recognised as a shell. Nothing named it.
func TestAgentTool_ALoginShellIsStillThatShell(t *testing.T) {
	t.Parallel()
	if got := agentTool("-claude"); got != "claude" {
		t.Errorf("agentTool(%q) = %q, want claude — the login-shell dash is not part of the name", "-claude", got)
	}
}

// ps prints the command in a column, and the column has to be cut by
// position. Searching for the third field found its first occurrence anywhere
// in the line, so a command beginning with digits the pid also contains was
// sliced from the wrong place — and the mangled command named no agent, which
// silently dropped every process under it.
func TestParseProcs_CutsTheCommandColumnByPosition(t *testing.T) {
	t.Parallel()
	table := parseProcs("51234     1 1234 --foo\n  600   500 claude\n")

	if got := table.command[51234]; got != "1234 --foo" {
		t.Errorf("command[51234] = %q, want %q", got, "1234 --foo")
	}
	if got := table.command[600]; got != "claude" {
		t.Errorf("command[600] = %q", got)
	}
}

// A pane row means what its field positions say, so a short row is not a row
// to salvage — and the last field, the path, keeps any separator inside it
// rather than becoming a sixth.
func TestParsePanes_TakesOnlyWholeRows(t *testing.T) {
	t.Parallel()
	got := parsePanes(strings.Join([]string{
		"%1\x1f100\x1fwork\x1f0\x1f/home/u/proj",
		"%2\x1f200\x1fwork",
		"%3\x1f300\x1fwork\x1f2",
		"",
	}, "\n"))

	if len(got) != 1 || got[0].id != "%1" {
		t.Fatalf("parsePanes kept %d rows: %+v", len(got), got)
	}
}

// A tmux session name may contain a tab. With a tab separator one there
// shifted every field after it, so the window index landed in the session and
// the pid landed in the window — and the pane was silently dropped or
// mislabelled. The separator is one no name will hold.
func TestParsePanes_ASeparatorlessTabInANameChangesNothing(t *testing.T) {
	t.Parallel()
	got := parsePanes("%1\x1f100\x1fmy\twork\x1f7\x1f/home/u/my proj\n")

	if len(got) != 1 {
		t.Fatalf("parsePanes kept %d rows", len(got))
	}
	if got[0].session != "my\twork" || got[0].window != "7" || got[0].dir != "/home/u/my proj" {
		t.Errorf("parsed %+v — the tab in the session name moved the other fields", got[0])
	}
}

// Discovery has to hand the picker a sorted list. SortAgents was tested on
// its own and called from one line in the UI: removing that line left an
// unsorted picker and the suite green, so the ordering is now part of
// discovery and tested through it.
func TestDiscover_OrdersWhatItFinds(t *testing.T) {
	t.Parallel()
	panes := parsePanes(strings.Join([]string{
		"%1\x1f100\x1fother\x1f1\x1f/elsewhere",
		"%2\x1f200\x1fmine\x1f1\x1f/repo",
		"",
	}, "\n"))
	procs := parseProcs("100 1 claude\n200 1 claude\n")

	got := discover(panes, procs, "", "mine", "/repo")

	if len(got) != 2 {
		t.Fatalf("discover found %d agents: %+v", len(got), got)
	}
	if got[0].Pane != "%2" {
		t.Errorf("the agent in differ's own session should come first, got %q", got[0].Pane)
	}
}

// Windows are numbered, and a string compare put 10 before 2.
func TestSortAgents_OrdersWindowsNumerically(t *testing.T) {
	t.Parallel()
	agents := []Agent{
		{Pane: "%a", Session: "s", Window: "10", Tool: "claude"},
		{Pane: "%b", Session: "s", Window: "2", Tool: "claude"},
	}
	SortAgents(agents, "", "")
	if agents[0].Window != "2" {
		t.Errorf("window order = %q then %q, want 2 then 10", agents[0].Window, agents[1].Window)
	}
}

// The own session comes out of the pane listing rather than a third
// subprocess. Asking tmux separately meant issue #84's "the whole scan is
// two subprocesses" was not met, while the answer was already in hand: the
// listing covers every pane on the server, differ's own included.
func TestSessionOf_ReadsTheOwnSessionFromTheListing(t *testing.T) {
	t.Parallel()
	panes := parsePanes(strings.Join([]string{
		"%1\x1f100\x1fwork\x1f0\x1f/a",
		"%7\x1f700\x1fmine\x1f3\x1f/b",
		"",
	}, "\n"))

	if got := sessionOf(panes, "%7"); got != "mine" {
		t.Errorf("sessionOf(%%7) = %q, want mine", got)
	}
	if got := sessionOf(panes, ""); got != "" {
		t.Errorf("sessionOf with no own pane = %q, want empty", got)
	}
	if got := sessionOf(panes, "%99"); got != "" {
		t.Errorf("sessionOf for a pane not in the listing = %q, want empty", got)
	}
}

// The real invocations of each agent, taken from their own documentation.
// Every one of these was missed: "run" was unconditionally treated as doing
// something *to* a package, and aider's package is named aider-chat.
func TestAgentTool_FindsTheDocumentedInvocations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ command, want string }{
		{"uvx --from aider-chat aider", "aider"},
		{"uv run aider", "aider"},
		{"uv tool run aider", "aider"},
		{"pipx run aider", "aider"},
		{"poetry run aider", "aider"},
		{"gh copilot suggest", "copilot"},

		// And `run` still means a manifest script where it does.
		{"npm run aider", ""},
		{"yarn run codex", ""},
		{"pnpm run claude", ""},
	} {
		if got := agentTool(tc.command); got != tc.want {
			t.Errorf("agentTool(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

// A one-shot agent is not a session to paste into — and `claude -p` is
// differ's own default commit_msg_cmd, so a second differ writing a commit
// message appeared in the picker for those seconds. A review pasted there
// goes into a process that has already read its stdin.
func TestAgentTool_AOneShotInvocationIsNotASession(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		"claude -p generate a commit message",
		"claude --print hello",
		"claude --help",
		"claude doctor",
		"claude mcp list",
		"codex --version",
	} {
		if got := agentTool(command); got != "" {
			t.Errorf("agentTool(%q) = %q, want none — there is no session there", command, got)
		}
	}
	// The interactive forms are still found.
	for _, command := range []string{"claude", "claude --model opus", "codex"} {
		if got := agentTool(command); got == "" {
			t.Errorf("agentTool(%q) found no agent", command)
		}
	}
}

// Installing an agent is not running one, and what stops it is the
// fall-through: the scan gives up at the first argument that is neither a
// tool nor a runner.
//
// A notRunning table of subcommands used to sit in front of this and was
// doing nothing — every fixture it was written for is stopped by the
// fall-through anyway, so deleting the whole branch left the suite green.
// These cases pin the mechanism that actually decides.
func TestAgentTool_InstallingIsToldFromRunning(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		"uv pip install aider",
		"pipx install aider",
		"npm install -g @anthropic-ai/claude-code",
		"yarn add opencode",
		"uv sync aider",
	} {
		if got := agentTool(command); got != "" {
			t.Errorf("agentTool(%q) = %q — installing an agent is not running one",
				command, got)
		}
	}
}

// A shell invoking the agent is matched at the shell itself, not only at the
// child it forks.
//
// `sh -c "<path>/claude; sleep 30"` is one command line as ps prints it, so
// strings.Fields yields "claude;" with the semicolon attached and the shell
// matches nothing — the agent was then found only because the child process
// had appeared in ps yet, which is a 400 ms race. It held on macOS and failed
// on ubuntu.
func TestAgentTool_AShellInvokingTheAgentIsMatchedAtTheShell(t *testing.T) {
	t.Parallel()
	if got := agentTool("sh -c /tmp/xyz/claude"); got != "claude" {
		t.Errorf(`agentTool("sh -c /tmp/xyz/claude") = %q, want claude`, got)
	}
	// And the shape that does not match, so the reason is recorded rather
	// than only the fix: a trailing command makes the path a different token.
	if got := agentTool("sh -c /tmp/xyz/claude; sleep 30"); got != "" {
		t.Errorf("a semicolon-joined command line matched %q; if this now "+
			"works the comment above is stale", got)
	}
}
