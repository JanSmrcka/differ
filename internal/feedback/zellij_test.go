package feedback

import (
	"context"
	"strings"
	"testing"
)

// Real `zellij action list-panes --json --all` rows, trimmed to the fields
// differ reads.
const zellijPanes = `[
 {"id":0,"is_plugin":true,"title":"tab-bar","exited":false,"tab_id":0,"tab_position":0,"tab_name":"Tab #1"},
 {"id":1,"is_plugin":false,"title":"differ","exited":false,"tab_id":0,"tab_position":0,"tab_name":"Tab #1","pane_command":"differ","pane_cwd":"/r/differ"},
 {"id":2,"is_plugin":false,"title":"✳ Zellij support","exited":false,"tab_id":0,"tab_position":0,"tab_name":"Tab #1","pane_command":"claude","pane_cwd":"/r/differ"},
 {"id":3,"is_plugin":false,"title":"codex","exited":false,"tab_id":1,"tab_position":1,"tab_name":"other","pane_command":"codex","pane_cwd":"/elsewhere"},
 {"id":4,"is_plugin":false,"title":"dead","exited":true,"tab_id":1,"tab_position":1,"tab_name":"other","pane_command":"claude","pane_cwd":"/r/differ"},
 {"id":5,"is_plugin":false,"title":"zsh","exited":false,"tab_id":1,"tab_position":1,"tab_name":"other","pane_command":"zsh","pane_cwd":"/r/differ"},
 {"id":6,"is_plugin":false,"title":"commit msg","exited":false,"tab_id":1,"tab_position":1,"tab_name":"other","pane_command":"claude -p","pane_cwd":"/r/differ"}
]`

func TestDetectMux_Zellij(t *testing.T) {
	t.Parallel()
	mux, err := DetectMux(envOf(map[string]string{"ZELLIJ": "0", "ZELLIJ_PANE_ID": "1"}))
	if err != nil {
		t.Fatal(err)
	}
	if mux.Name() != "zellij" || mux.Self() != "terminal_1" {
		t.Errorf("got %s, self %q", mux.Name(), mux.Self())
	}
	// ZELLIJ is set in every pane, so it alone must not beat an inner tmux.
	mux, _ = DetectMux(envOf(map[string]string{"ZELLIJ": "0", "ZELLIJ_PANE_ID": "1", "TMUX": "x", "TERM_PROGRAM": "tmux"}))
	if mux.Name() != "tmux" {
		t.Errorf("tmux inside zellij: got %s", mux.Name())
	}
}

func TestDiscoverZellij_FindsAgentsByPaneCommand(t *testing.T) {
	t.Parallel()
	got, err := discoverZellij([]byte(zellijPanes), "terminal_1", RepoInfo{Root: "/r/differ"})
	if err != nil {
		t.Fatal(err)
	}
	var panes []string
	for _, a := range got {
		panes = append(panes, a.Pane+"="+a.Tool)
	}
	// Not the plugin, differ's own pane, an exited pane, a shell or a one-shot.
	want := "terminal_2=claude terminal_3=codex"
	if strings.Join(panes, " ") != want {
		t.Errorf("got %v, want %s", panes, want)
	}
	if got[0].Dir != "/r/differ" || got[0].Mux != "zellij" || got[0].Session != "Tab #1" {
		t.Errorf("fields: %+v", got[0])
	}
}

func TestZellijAgent_ConfigRoundTrip(t *testing.T) {
	t.Parallel()
	a := Agent{Pane: "terminal_2", Mux: "zellij"}
	cfg := a.FeedbackConfig()
	if cfg.Target != "zellij" || cfg.ZellijTarget != "terminal_2" {
		t.Fatalf("got %+v", cfg)
	}
	if !a.Matches(cfg) || a.Matches(Config{ZellijTarget: "terminal_3"}) {
		t.Error("Matches should follow zellij_target")
	}
}

func TestZellijTarget_RefusesWithoutAPaneOrToSendToItself(t *testing.T) {
	t.Parallel()
	if _, err := Resolve(Config{Target: "zellij", Env: envOf(nil)}); err == nil {
		t.Error("an empty zellij_target has no default to fall back on")
	}
	tr := &zellijTarget{pane: "terminal_1", self: "terminal_1"}
	if err := tr.Send(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "own pane") {
		t.Errorf("got %v", err)
	}
}

func TestZellijPaneID_Normalises(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"3": "terminal_3", "terminal_3": "terminal_3", " 3\n": "terminal_3", "": ""} {
		if got := zellijPaneID(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestRunZellij_MissingBinarySaysSo(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := runZellij(context.Background(), "list-panes")
	if err == nil || !strings.Contains(err.Error(), "not installed or not on differ's PATH") {
		t.Errorf("got %v", err)
	}
}
