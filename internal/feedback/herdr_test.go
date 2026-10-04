package feedback

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The listings herdr 0.9.3 actually printed, with the paths anonymised.
func herdrFixtures(t *testing.T) (agents, workspaces []byte) {
	t.Helper()
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	return read("herdr_agent_list.json"), read("herdr_workspace_list.json")
}

// One listing says everything agents.go reconstructs from tmux and ps — and
// what it cannot: the agent's state, its own session id, and what it is
// working on in its own words.
func TestDiscoverHerdr_ReadsWhatHerdrReports(t *testing.T) {
	t.Parallel()
	agentsJSON, workspacesJSON := herdrFixtures(t)

	found, err := discoverHerdr(agentsJSON, workspacesJSON, "w3:p1", "w3", RepoInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("got %d agents, want 2 (differ's own pane excluded): %+v", len(found), found)
	}
	byPane := map[string]Agent{}
	for _, a := range found {
		byPane[a.Pane] = a
		if a.Pane == "w3:p1" {
			t.Error("offered differ's own pane")
		}
	}
	got := byPane["w2:p2"]
	want := Agent{
		Pane: "w2:p2", Tool: "claude", Dir: "/home/u/git/differ", Mux: "herdr",
		Workspace: "differ", WorkspaceID: "w2", State: "working",
		Title:     "Session search across workspaces",
		SessionID: "01729727-58c0-46f8-90ba-5cb2723b2a5a",
	}
	if got != want {
		t.Errorf("w2:p2 =\n %+v\nwant\n %+v", got, want)
	}
	if l := got.Label(); l != "differ" {
		t.Errorf("Label = %q, want the workspace label", l)
	}
}

// The error a herdr command prints is JSON on stderr; its code is what decides
// whether a pane has gone or an agent is busy, so it has to survive.
func TestParseHerdrError_KeepsTheCode(t *testing.T) {
	t.Parallel()
	err := parseHerdrError([]byte(`{"error":{"code":"agent_not_found","message":"agent target w99:p9 not found"},"id":"cli:agent:prompt"}`), nil)
	he, ok := err.(*HerdrError)
	if !ok {
		t.Fatalf("got %T %v, want *HerdrError", err, err)
	}
	if he.Code != "agent_not_found" || he.Message != "agent target w99:p9 not found" {
		t.Errorf("got %+v", he)
	}
}

// An agent that is waiting is the one a review is for: blocked, idle and done
// sort above working, and unknown — an agent herdr has no integration for —
// is still offered, last. Within a state, differ's own workspace and then its
// own repository come first.
func TestSortHerdrAgents_PutsTheWaitingOneFirst(t *testing.T) {
	t.Parallel()
	agents := []Agent{
		{Pane: "w9:p1", State: "unknown", WorkspaceID: "w3"},
		{Pane: "w1:p1", State: "working", WorkspaceID: "w1"},
		{Pane: "w2:p1", State: "idle", WorkspaceID: "w2"},
		{Pane: "w4:p1", State: "idle", WorkspaceID: "w4", Dir: "/r/sub"},
		{Pane: "w3:p2", State: "idle", WorkspaceID: "w3"},
		{Pane: "w5:p1", State: "blocked", WorkspaceID: "w5"},
		{Pane: "w6:p1", State: "done", WorkspaceID: "w6"},
	}
	repoOf := map[string]string{"w4": "/r/.git"}
	sortHerdrAgents(agents, "w3", RepoInfo{Root: "/elsewhere", CommonDir: "/r/.git"}, repoOf)

	var got []string
	for _, a := range agents {
		got = append(got, a.Pane)
	}
	want := []string{"w3:p2", "w4:p1", "w2:p1", "w5:p1", "w6:p1", "w1:p1", "w9:p1"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("order = %v\nwant    %v", got, want)
	}
}

func readLog(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func newHerdrTargetFor(t *testing.T, bin string, cfg Config) Target {
	t.Helper()
	cfg.Target = "herdr"
	cfg.Env = herdrEnv(bin)
	target, err := Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

// The choice is stored as the agent's own session id, which survives a pane
// move; the pane is whatever herdr says that session is in now.
func TestHerdrTarget_SubmitsToTheSessionsCurrentPane(t *testing.T) {
	bin, log := fakeHerdr(t, "")
	target := newHerdrTargetFor(t, bin, Config{
		HerdrTarget: "01729727-58c0-46f8-90ba-5cb2723b2a5a", HerdrPane: "w7:p7",
	})
	if err := target.Send(context.Background(), "line one\nline two"); err != nil {
		t.Fatal(err)
	}
	calls := readLog(t, log)
	last := calls[len(calls)-1]
	// Submitted, not pasted: herdr refuses on differ's behalf when the pane
	// is not an agent that can take a prompt, which is the one thing tmux
	// could not do and the reason the tmux path does not press Enter.
	if !strings.HasPrefix(last, "agent prompt w2:p2 ") {
		t.Errorf("last call = %q, want agent prompt to w2:p2", last)
	}
	if !strings.Contains(last, "--wait") || !strings.Contains(last, "--until working") {
		t.Errorf("prompt does not wait for the agent to pick it up: %q", last)
	}
}

// A session herdr no longer lists falls back to the pane it was in.
func TestHerdrTarget_FallsBackToThePane(t *testing.T) {
	bin, log := fakeHerdr(t, "")
	target := newHerdrTargetFor(t, bin, Config{HerdrTarget: "gone", HerdrPane: "w1:p3"})
	if err := target.Send(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	calls := readLog(t, log)
	if last := calls[len(calls)-1]; !strings.HasPrefix(last, "agent prompt w1:p3 ") {
		t.Errorf("last call = %q", last)
	}
}

// herdr rejects a prompt to a blocked agent before sending anything. That is
// a failure — the comments stay pending — and its code survives to the UI.
func TestHerdrTarget_ABlockedAgentIsAFailure(t *testing.T) {
	bin, _ := fakeHerdr(t, `if [ "$1 $2" = "agent prompt" ]; then
  echo '{"error":{"code":"agent_blocked","message":"agent is blocked"},"id":"cli:agent:prompt"}' >&2; exit 1
fi
`)
	target := newHerdrTargetFor(t, bin, Config{HerdrPane: "w1:p3"})
	err := target.Send(context.Background(), "x")
	var he *HerdrError
	if !errors.As(err, &he) || he.Code != "agent_blocked" {
		t.Fatalf("err = %v, want agent_blocked", err)
	}
}

// Stalled means the prompt was submitted and herdr did not see the agent
// start within its five seconds. The text is in the agent's hands, so this is
// a delivery, not a failure to retry — retrying would send it twice.
func TestHerdrTarget_AStalledPromptWasDelivered(t *testing.T) {
	bin, _ := fakeHerdr(t, `if [ "$1 $2" = "agent prompt" ]; then
  echo '{"error":{"code":"agent_prompt_stalled","message":"no activity"},"id":"cli:agent:prompt"}' >&2; exit 1
fi
`)
	target := newHerdrTargetFor(t, bin, Config{HerdrPane: "w1:p3"})
	if err := target.Send(context.Background(), "x"); err != nil {
		t.Errorf("stalled prompt reported as failed: %v", err)
	}
	// But nothing was seen working, so there is nothing to wait for: the
	// agent never left idle, and a wait would match that at once and
	// report an answer nobody gave.
	if seen := target.(Watcher).Seen(); seen != "" {
		t.Errorf("Seen after a stall = %q, want nothing", seen)
	}
}

// What the prompt's --wait matched is what the send saw: working, or blocked
// when the agent went straight to a question.
func TestHerdrTarget_SeenIsWhatThePromptMatched(t *testing.T) {
	bin, _ := fakeHerdr(t, `if [ "$1 $2" = "agent prompt" ]; then
  echo '{"id":"cli:agent:prompt","result":{"agent":{"agent_status":"blocked","pane_id":"w1:p3"},"type":"agent_info"}}'; exit 0
fi
`)
	target := newHerdrTargetFor(t, bin, Config{HerdrPane: "w1:p3"})
	if err := target.Send(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if seen := target.(Watcher).Seen(); seen != "blocked" {
		t.Errorf("Seen = %q, want blocked", seen)
	}

	// Output it cannot read still means --wait matched working or blocked;
	// working is the one that needs following.
	bin, _ = fakeHerdr(t, "")
	target = newHerdrTargetFor(t, bin, Config{HerdrPane: "w1:p3"})
	if err := target.Send(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if seen := target.(Watcher).Seen(); seen != "working" {
		t.Errorf("Seen with unreadable output = %q, want working", seen)
	}
}

// After a send, the target can wait for the agent to finish with it.
func TestHerdrTarget_WaitsForTheAgentToSettle(t *testing.T) {
	bin, log := fakeHerdr(t, `if [ "$1 $2" = "agent wait" ]; then
  echo '{"id":"cli:agent:wait","result":{"agent":{"agent_status":"done","pane_id":"w1:p3"},"type":"agent_info"}}'; exit 0
fi
`)
	target := newHerdrTargetFor(t, bin, Config{HerdrPane: "w1:p3"})
	if err := target.Send(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	w, ok := target.(Watcher)
	if !ok {
		t.Fatal("herdr target is not a Watcher")
	}
	state, err := w.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state != "done" {
		t.Errorf("state = %q, want done", state)
	}
	calls := readLog(t, log)
	last := calls[len(calls)-1]
	for _, want := range []string{"agent wait w1:p3", "--until idle", "--until done", "--until blocked"} {
		if !strings.Contains(last, want) {
			t.Errorf("wait call %q lacks %q", last, want)
		}
	}
}

// Quitting differ cancels the wait, and the herdr child goes with it.
func TestHerdrTarget_CancellingTheWaitReturns(t *testing.T) {
	bin, _ := fakeHerdr(t, `if [ "$1 $2" = "agent wait" ]; then exec sleep 30; fi
`)
	target := newHerdrTargetFor(t, bin, Config{HerdrPane: "w1:p3"})
	if err := target.Send(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := target.(Watcher).Wait(ctx)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("cancelled wait returned no error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled wait did not return")
	}
}

// Against the herdr this runs in, if any. Read-only — only the listings — and
// skipped everywhere else, CI included, so it runs when someone runs it.
func TestHerdrMux_AgainstARealServer(t *testing.T) {
	if os.Getenv("HERDR_ENV") != "1" {
		t.Skip("not inside herdr")
	}
	mux, err := DetectMux(Env{})
	if err != nil {
		t.Fatal(err)
	}
	if mux.Name() != "herdr" {
		t.Skipf("innermost multiplexer is %s", mux.Name())
	}
	found, err := mux.Agents(context.Background(), RepoInfo{})
	if err != nil {
		t.Fatalf("Agents: %v", err)
	}
	for _, a := range found {
		if a.Pane == mux.Self() {
			t.Errorf("offered differ's own pane %s", a.Pane)
		}
		if a.Pane == "" || a.Tool == "" || a.State == "" {
			t.Errorf("incomplete agent %+v", a)
		}
	}
}
