package feedback

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Mux is a terminal multiplexer differ can find agents in.
//
// tmux had three pieces of knowledge — list the places an agent could be,
// find the agent, put text in front of it — and only the last was behind an
// interface (Target). Discovery is this one, so the picker asks a Mux and
// never learns which it is talking to.
type Mux interface {
	// Name is "tmux", "herdr" or "zellij", which is also the feedback_target value
	// that sends there.
	Name() string
	// Agents lists the agents a review could go to, most likely first.
	Agents(ctx context.Context, repo RepoInfo) ([]Agent, error)
	// Self is differ's own pane, which is never a destination.
	Self() string
	// Searched says, in lines short enough for the picker, where Agents
	// looked — so an empty list reads as "nothing here", not "broken".
	Searched() []string
}

// RepoInfo is what discovery knows about the repository differ is reviewing,
// for putting the agents working in it first.
type RepoInfo struct {
	// Root is the worktree's top level.
	Root string
	// CommonDir is the repository's shared .git directory — the same for
	// every linked worktree, and what herdr calls repo_key.
	CommonDir string
}

// Env is the environment detection reads. Injected rather than read inside,
// so tests need no t.Setenv, which would bar t.Parallel. The zero value reads
// the process environment.
type Env struct {
	Getenv func(string) string
}

func (e Env) get(key string) string {
	if e.Getenv == nil {
		return os.Getenv(key)
	}
	return e.Getenv(key)
}

// DetectMux names the multiplexer differ is running in.
//
// Detection, not configuration: feedback_target written by hand was what #84
// was filed about. herdr injects HERDR_ENV and HERDR_PANE_ID into every pane
// and tmux sets TMUX, so this is an environment read with no subprocess. With
// both — one running inside the other — the innermost is the terminal
// differ's own pane belongs to, and TERM_PROGRAM is set by whichever that is.
func DetectMux(env Env) (Mux, error) {
	inHerdr := env.get("HERDR_ENV") == "1" && env.get("HERDR_PANE_ID") != ""
	inTmux := env.get("TMUX") != ""
	inZellij := env.get("ZELLIJ") != "" && env.get("ZELLIJ_PANE_ID") != ""
	switch {
	case inHerdr && inTmux && env.get("TERM_PROGRAM") == "tmux":
		return newTmuxMux(env), nil
	case inHerdr:
		return newHerdrMux(env), nil
	case inTmux:
		return newTmuxMux(env), nil
	case inZellij:
		return newZellijMux(env), nil
	}
	return nil, fmt.Errorf("differ is not running in a multiplexer — it looked for herdr (HERDR_ENV), tmux (TMUX) and zellij (ZELLIJ)")
}

type tmuxMux struct{ self string }

func newTmuxMux(env Env) *tmuxMux { return &tmuxMux{self: env.get("TMUX_PANE")} }

func (m *tmuxMux) Name() string { return "tmux" }
func (m *tmuxMux) Self() string { return m.self }
func (m *tmuxMux) Searched() []string {
	return []string{
		"differ looks for claude, codex, gemini,",
		"copilot, opencode and aider in every pane",
		"on this tmux server.",
	}
}
func (m *tmuxMux) Agents(ctx context.Context, repo RepoInfo) ([]Agent, error) {
	return Agents(ctx, repo.Root)
}

type herdrMux struct {
	bin, self, selfWS string
}

func newHerdrMux(env Env) *herdrMux {
	return &herdrMux{bin: herdrBin(env), self: env.get("HERDR_PANE_ID"), selfWS: env.get("HERDR_WORKSPACE_ID")}
}

// herdrBin is the herdr executable: the one herdr says it is, when differ
// runs inside it, so a herdr not on PATH still works.
func herdrBin(env Env) string {
	if bin := env.get("HERDR_BIN_PATH"); bin != "" {
		return bin
	}
	return "herdr"
}

func (m *herdrMux) Name() string { return "herdr" }
func (m *herdrMux) Self() string { return m.self }
func (m *herdrMux) Searched() []string {
	return []string{
		"herdr lists the agents its integrations",
		"recognise, in every workspace.",
	}
}

// Agents asks for the agents and the workspaces at once. The second listing
// is what carries each workspace's label and repo_key; asked in parallel the
// pair costs one call's wall time.
func (m *herdrMux) Agents(ctx context.Context, repo RepoInfo) ([]Agent, error) {
	type answer struct {
		out []byte
		err error
	}
	ws := make(chan answer, 1)
	go func() {
		out, err := runHerdr(ctx, m.bin, "workspace", "list")
		ws <- answer{out, err}
	}()
	agentsOut, err := runHerdr(ctx, m.bin, "agent", "list")
	w := <-ws
	if err != nil {
		return nil, err
	}
	if w.err != nil {
		return nil, w.err
	}
	return discoverHerdr(agentsOut, w.out, m.self, m.selfWS, repo)
}

// runHerdr runs one herdr command and returns its stdout, or herdr's own
// error from stderr.
func runHerdr(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("herdr %s: %w", args[0], ctx.Err())
		}
		return nil, parseHerdrError(stderr.Bytes(), err)
	}
	return stdout.Bytes(), nil
}

// FeedbackConfig is the configuration that sends a review to this agent.
func (a Agent) FeedbackConfig() Config {
	if a.Mux == "herdr" {
		return Config{Target: "herdr", HerdrTarget: a.SessionID, HerdrPane: a.Pane}
	}
	if a.Mux == "zellij" {
		return Config{Target: "zellij", ZellijTarget: a.Pane}
	}
	return Config{Target: "tmux", TmuxTarget: a.Pane}
}

// Matches reports whether cfg names this agent — the picker's "in use".
//
// By identity alone, not by cfg.Target: the tmux picker always marked the
// pane tmux_target names, whatever feedback_target said, and the two
// namespaces ("%4", "w2:p2") cannot be mistaken for each other.
func (a Agent) Matches(cfg Config) bool {
	want := a.FeedbackConfig()
	if want.Target == "zellij" {
		return want.ZellijTarget != "" && want.ZellijTarget == cfg.ZellijTarget
	}
	if want.Target == "herdr" {
		if want.HerdrTarget != "" && cfg.HerdrTarget != "" {
			return want.HerdrTarget == cfg.HerdrTarget
		}
		return want.HerdrPane == cfg.HerdrPane
	}
	return want.TmuxTarget != "" && want.TmuxTarget == cfg.TmuxTarget
}
