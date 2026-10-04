package feedback

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(vars map[string]string) Env {
	return Env{Getenv: func(k string) string { return vars[k] }}
}

// Detection is an environment read, never a subprocess: herdr injects
// HERDR_ENV and HERDR_PANE_ID into every pane, tmux sets TMUX. With both, the
// innermost terminal is the one TERM_PROGRAM names.
func TestDetectMux_ReadsTheEnvironment(t *testing.T) {
	t.Parallel()
	herdr := map[string]string{"HERDR_ENV": "1", "HERDR_PANE_ID": "w3:p1"}
	tmux := map[string]string{"TMUX": "/tmp/tmux-501/default,1,0", "TMUX_PANE": "%3"}
	both := func(term string) map[string]string {
		m := map[string]string{"TERM_PROGRAM": term}
		for k, v := range herdr {
			m[k] = v
		}
		for k, v := range tmux {
			m[k] = v
		}
		return m
	}
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"herdr", herdr, "herdr"},
		{"tmux", tmux, "tmux"},
		{"tmux inside herdr", both("tmux"), "tmux"},
		{"herdr inside tmux", both("herdr"), "herdr"},
	} {
		mux, err := DetectMux(envOf(tc.env))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if mux.Name() != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, mux.Name(), tc.want)
		}
	}
}

// With neither, the error says what was looked for: "nothing here" and
// "differ is broken" read the same otherwise.
func TestDetectMux_NeitherSaysWhatWasLookedFor(t *testing.T) {
	t.Parallel()
	_, err := DetectMux(envOf(nil))
	if err == nil {
		t.Fatal("no error outside any multiplexer")
	}
	for _, want := range []string{"herdr", "tmux"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

// fakeHerdr writes a stand-in for the herdr binary that answers the two
// listings from the fixtures and records every invocation's argv, one per
// line, to the returned log.
//
// A test that execs it must not be t.Parallel. On Linux, exec'ing a file just
// written fails with ETXTBSY when a fork in another goroutine has inherited
// the descriptor (golang/go#22315, the editor tests' CI failure). Go holds
// parallel tests until the sequential ones have returned, so a sequential
// test has no concurrent fork to race.
func fakeHerdr(t *testing.T, extra string) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "argv.log")
	agents, _ := filepath.Abs(filepath.Join("testdata", "herdr_agent_list.json"))
	workspaces, _ := filepath.Abs(filepath.Join("testdata", "herdr_workspace_list.json"))
	script := "#!/bin/sh\n" +
		"printf '%s' \"$*\" | tr '\\n' ' ' >> '" + log + "'; echo >> '" + log + "'\n" +
		extra +
		"case \"$1 $2\" in\n" +
		"  'agent list') cat '" + agents + "' ;;\n" +
		"  'workspace list') cat '" + workspaces + "' ;;\n" +
		"esac\n"
	bin = filepath.Join(dir, "herdr")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func herdrEnv(bin string) Env {
	return envOf(map[string]string{
		"HERDR_ENV": "1", "HERDR_PANE_ID": "w3:p1", "HERDR_WORKSPACE_ID": "w3",
		"HERDR_BIN_PATH": bin,
	})
}

func TestHerdrMux_ListsAgentsThroughTheCLI(t *testing.T) {
	bin, _ := fakeHerdr(t, "")
	mux, err := DetectMux(herdrEnv(bin))
	if err != nil {
		t.Fatal(err)
	}
	if mux.Self() != "w3:p1" {
		t.Errorf("Self = %q", mux.Self())
	}
	found, err := mux.Agents(context.Background(), RepoInfo{CommonDir: "/home/u/git/differ/.git"})
	if err != nil {
		t.Fatal(err)
	}
	var panes []string
	for _, a := range found {
		panes = append(panes, a.Pane)
	}
	// w1 is idle, so it is first; w2 is working in this repository.
	if strings.Join(panes, " ") != "w1:p3 w2:p2" {
		t.Errorf("panes = %v", panes)
	}
}

// Choosing an agent produces the config that sends to it, whichever
// multiplexer found it — so the picker never branches on the name.
func TestAgent_FeedbackConfigSendsToIt(t *testing.T) {
	t.Parallel()
	tm := Agent{Pane: "%4", Mux: "tmux"}
	if got := tm.FeedbackConfig(); got.Target != "tmux" || got.TmuxTarget != "%4" {
		t.Errorf("tmux: %+v", got)
	}
	hd := Agent{Pane: "w2:p2", Mux: "herdr", SessionID: "abc"}
	if got := hd.FeedbackConfig(); got.Target != "herdr" || got.HerdrTarget != "abc" || got.HerdrPane != "w2:p2" {
		t.Errorf("herdr: %+v", got)
	}

	if !tm.Matches(Config{Target: "tmux", TmuxTarget: "%4"}) {
		t.Error("tmux agent does not match its own config")
	}
	if tm.Matches(Config{Target: "herdr", HerdrPane: "%4"}) {
		t.Error("tmux agent matches a herdr config")
	}
	// The session id is the identity; the pane is only a fallback.
	if !hd.Matches(Config{Target: "herdr", HerdrTarget: "abc", HerdrPane: "w9:p9"}) {
		t.Error("herdr agent moved panes and no longer matches")
	}
	if !(Agent{Pane: "w2:p2", Mux: "herdr"}).Matches(Config{Target: "herdr", HerdrPane: "w2:p2"}) {
		t.Error("herdr agent without a session does not match by pane")
	}
}
