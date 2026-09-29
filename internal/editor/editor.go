// Package editor opens a file from differ in the user's editor without ending
// differ.
//
// It imports only the standard library: no bubbletea, and nothing from
// internal/ui. The one thing it cannot do is hand the terminal to a child
// process — that is the program's job — so a plan that needs the terminal is
// returned as an argv for the caller to run, and everything else executes
// here.
package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Env is the process environment the decision depends on. It is read once at
// the edge by NewEnv so every choice below is a pure function of its inputs:
// tests can then describe a whole situation without t.Setenv, which would bar
// them from t.Parallel.
type Env struct {
	Editor        string // $EDITOR
	InTmux        bool   // $TMUX is non-empty
	TmuxPane      string // $TMUX_PANE — differ's own pane
	TmpDir        string // $TMPDIR — where nvim listens on macOS
	XDGRuntimeDir string // $XDG_RUNTIME_DIR — where nvim listens on Linux
	User          string // $USER — part of nvim's socket directory name
}

// NewEnv reads the environment. This is the only place in the package that
// touches os.Getenv.
func NewEnv() Env {
	return Env{
		Editor:        os.Getenv("EDITOR"),
		InTmux:        os.Getenv("TMUX") != "",
		TmuxPane:      os.Getenv("TMUX_PANE"),
		TmpDir:        os.Getenv("TMPDIR"),
		XDGRuntimeDir: os.Getenv("XDG_RUNTIME_DIR"),
		User:          currentUser(os.Getenv("USER")),
	}
}

// currentUser is the login name nvim builds its socket directory from.
// $USER is unset in containers and under systemd, so fall back to asking.
func currentUser(fromEnv string) string {
	if fromEnv != "" {
		return fromEnv
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

// Config mirrors the two configuration fields this package cares about rather
// than importing internal/config, exactly as feedback.Config does.
type Config struct {
	// Cmd is editor_cmd, with {file}, {repo} and {line} placeholders.
	Cmd string
	// Strategy is editor_strategy; empty means auto.
	Strategy string
	// Grace is how long a detached editor is watched for an immediate
	// failure. Zero means detachGrace.
	Grace time.Duration
}

// Request is the situation: what to open, and where differ is running.
type Request struct {
	File string // repo-relative path under the cursor
	Repo string // absolute repo root, from git.Repo.Dir()
	Line int    // 1-based line in the file on disk; 0 when unknown
	Env  Env
}

// abs is the file the request points at.
func (r Request) abs() string { return filepath.Join(r.Repo, r.File) }

// Strategy names the mechanism chosen. It is exported so a test can assert
// which path Resolve picked without executing anything.
type Strategy string

const (
	StrategyAuto   Strategy = "auto"   // reuse, else a new window, else inline
	StrategyReuse  Strategy = "reuse"  // an editor already open in this session
	StrategyWindow Strategy = "window" // a new tmux window
	StrategyInline Strategy = "inline" // take over differ's terminal
	// StrategyDetach runs the editor in the background and returns at once.
	// It is for editors that need no terminal and reuse their own window —
	// VS Code, Zed, Sublime. differ does not guess which editor is which, so
	// this is only ever chosen explicitly.
	StrategyDetach Strategy = "detach"
	// StrategyCustom is an editor_cmd that is itself a tmux command. It is
	// not an editor invocation but a mechanism, so it runs as written instead
	// of being wrapped in one of ours. It cannot be configured.
	StrategyCustom Strategy = "custom"
)

// Strategies lists the values a user may configure, for error messages.
func Strategies() []string {
	return []string{
		string(StrategyAuto), string(StrategyReuse),
		string(StrategyWindow), string(StrategyInline),
		string(StrategyDetach),
	}
}

// Kind says how the caller must run a plan.
type Kind int

const (
	// KindTerminal needs differ's terminal. The caller must run Argv in Dir
	// with tea.ExecProcess, so differ suspends and resumes around it.
	KindTerminal Kind = iota
	// KindDetached touches nothing the TUI owns, so Run may be called from an
	// ordinary tea.Cmd.
	KindDetached
)

// Plan is a decided course of action.
type Plan struct {
	Kind     Kind
	Strategy Strategy
	// Argv is the command, and Dir where it runs. KindTerminal plans are run
	// from these by the caller; for KindDetached they describe what Run does.
	Argv []string
	Dir  string
	// Desc is the status line for a success. Empty means say nothing.
	Desc string

	run func(context.Context) error
}

// Run performs a KindDetached plan.
func (p Plan) Run(ctx context.Context) error {
	if p.run == nil {
		return fmt.Errorf("%s does not run on its own", p.Strategy)
	}
	return p.run(ctx)
}

// Resolve picks a strategy and returns the plan. Errors carry text already fit
// for differ's status bar — the caller never reformats them.
func Resolve(ctx context.Context, cfg Config, req Request) (Plan, error) {
	if req.File == "" {
		return Plan{}, errors.New("no file selected")
	}
	// A deleted file is still in the diff. Opening it would give an empty
	// buffer, and one :w would resurrect it empty — so refuse. Stat rather
	// than trusting the file's git status: the agent may have removed it
	// since differ last polled.
	if _, err := os.Stat(req.abs()); err != nil {
		return Plan{}, fmt.Errorf("%s is gone — nothing to edit", req.File)
	}

	want, err := wantedStrategy(cfg.Strategy)
	if err != nil {
		return Plan{}, err
	}

	argv := buildArgv(cfg.Cmd, req)
	if _, err := exec.LookPath(argv[0]); err != nil {
		return Plan{}, fmt.Errorf("editor %q not found on PATH — set editor_cmd or $EDITOR", argv[0])
	}

	// An editor_cmd that is itself a tmux command is already a mechanism;
	// wrapping it in another one would nest tmux inside tmux. It wins over
	// editor_strategy outright.
	if filepath.Base(argv[0]) == "tmux" {
		return inlinePlan(argv, req), nil
	}
	return planFor(ctx, cfg, want, argv, req)
}

func wantedStrategy(s string) (Strategy, error) {
	switch st := Strategy(strings.TrimSpace(s)); st {
	case "", StrategyAuto:
		return StrategyAuto, nil
	case StrategyReuse, StrategyWindow, StrategyInline, StrategyDetach:
		return st, nil
	default:
		return "", fmt.Errorf("unknown editor_strategy %q — use one of: %s",
			s, strings.Join(Strategies(), ", "))
	}
}

// planFor turns a wanted strategy into a plan.
//
// An explicitly chosen strategy that cannot be honoured is an error rather
// than a silent downgrade — the same call internal/feedback makes when its
// tmux target is unavailable. Only auto falls back, because falling back is
// what auto means.
func planFor(ctx context.Context, cfg Config, want Strategy, argv []string, req Request) (Plan, error) {
	switch want {
	case StrategyInline:
		return inlinePlan(argv, req), nil
	case StrategyDetach:
		return detachPlan(argv, req, cfg.Grace), nil
	}
	if !req.Env.InTmux {
		if want == StrategyAuto {
			return inlinePlan(argv, req), nil
		}
		return Plan{}, fmt.Errorf(
			"editor_strategy is %q but differ is not running inside tmux — set editor_strategy to inline", want)
	}

	if want == StrategyReuse || want == StrategyAuto {
		plan, err := reusePlan(ctx, req)
		switch {
		case err == nil:
			return plan, nil
		case want == StrategyReuse:
			return Plan{}, err
		}
	}
	return windowPlan(argv, req), nil
}

func inlinePlan(argv []string, req Request) Plan {
	strategy := StrategyInline
	if filepath.Base(argv[0]) == "tmux" {
		strategy = StrategyCustom
	}
	return Plan{Kind: KindTerminal, Strategy: strategy, Argv: argv, Dir: req.Repo}
}

// detachGrace is how long a detached editor is watched before it is declared
// launched. Long enough to catch one that dies on the spot — a bad flag, a
// missing profile — and short enough not to delay the status line.
const detachGrace = 300 * time.Millisecond

// detachWaitDelay bounds how long Wait lingers over pipes the launcher's
// children inherited. code, zed and subl all exit at once on a cold start and
// leave a GUI grandchild holding stdout, which without this makes Wait block
// for the editor's whole lifetime.
const detachWaitDelay = 100 * time.Millisecond

// detachPlan starts the editor and leaves it running.
//
// It must not wait for the editor: these are editors that own their own
// window, and the whole point is that differ carries on. Waiting had two
// failure modes, both measured — a launcher that stays in the foreground
// (gvim, emacs, `code --wait`, the JetBrains launcher with no instance up)
// was killed at the timeout, and one whose grandchild held the inherited
// pipes blocked for as long as the editor lived.
//
// Only stderr is collected, and only for long enough to report an editor that
// fails immediately, so a typo in editor_cmd does not vanish in silence.
func detachPlan(argv []string, req Request, grace time.Duration) Plan {
	if grace <= 0 {
		grace = detachGrace
	}
	return Plan{
		Kind:     KindDetached,
		Strategy: StrategyDetach,
		Argv:     argv,
		Dir:      req.Repo,
		Desc:     "opened " + req.File,
		run: func(ctx context.Context) error {
			cmd := exec.Command(argv[0], argv[1:]...)
			cmd.Dir = req.Repo
			// Stdin and Stdout stay nil, so the child gets /dev/null and
			// cannot hold a pipe of ours open.
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			cmd.WaitDelay = detachWaitDelay

			if err := cmd.Start(); err != nil {
				return fmt.Errorf("%s: %w", filepath.Base(argv[0]), err)
			}

			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()

			select {
			case err := <-done:
				// ErrWaitDelay only means a child outlived the launcher and
				// kept the pipe; the editor started fine.
				if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
					return fmt.Errorf("%s: %w%s", filepath.Base(argv[0]), err,
						stderrText(stderr.String()))
				}
				return nil
			case <-time.After(grace):
				// Still running, which for a detached editor is success.
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
}

// stderrText renders collected stderr ready to append to an error.
func stderrText(s string) string {
	if s = strings.TrimSpace(s); s != "" {
		return ": " + strings.ReplaceAll(s, "\n", " ")
	}
	return ""
}

// buildArgv expands the placeholders in tmpl and splits it into an argv.
//
// The split happens on the template, before expansion. cmd/root.go split the
// expanded string, which turned /a/b/my file.go into two arguments;
// placeholders are single tokens, so splitting first cannot.
//
// It cannot fail: an empty template falls back to $EDITOR and then to vi, so
// there is always a first element. Whether that element is a program you can
// actually run is Resolve's question, not this one's.
func buildArgv(tmpl string, req Request) []string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		tmpl = strings.TrimSpace(req.Env.Editor)
	}
	if tmpl == "" {
		tmpl = "vi"
	}
	fields := strings.Fields(tmpl)

	// Expansion is per token, after the split. That is what keeps a path with
	// a space in it a single argument.
	//
	// An unknown line becomes 1 rather than being dropped: the template owns
	// the syntax, and "{file}:{line}" has to stay well formed when e is
	// pressed from the file list, where there is no line.
	line := req.Line
	if line < 1 {
		line = 1
	}
	expand := strings.NewReplacer(
		"{file}", req.abs(),
		"{repo}", req.Repo,
		"{line}", strconv.Itoa(line),
	)
	argv := make([]string, 0, len(fields)+1)
	for _, f := range fields {
		argv = append(argv, expand.Replace(f))
	}
	if !strings.Contains(tmpl, "{file}") {
		argv = append(argv, fileArgs(argv[0], req.abs(), req.Line)...)
	}
	return argv
}

// fileArgs is how a given editor is told which file, and where in it.
//
// It only runs for a template that did not place {file} itself. Once the
// author lays out the arguments there is nowhere safe to slip a line flag in,
// so such a template uses {line} instead.
//
// Only families that can be checked on a developer machine are claimed —
// emacs, helix and sublime are left to {line} rather than guessed at.
func fileArgs(prog, file string, line int) []string {
	if line < 1 {
		return []string{file}
	}
	switch filepath.Base(prog) {
	case "vi", "vim", "nvim", "view", "nano":
		return []string{"+" + strconv.Itoa(line), file}
	case "code", "code-insiders":
		return []string{"--goto", file + ":" + strconv.Itoa(line)}
	default:
		return []string{file}
	}
}
