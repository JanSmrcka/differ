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
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
		User:          os.Getenv("USER"),
	}
}

// Config mirrors the two configuration fields this package cares about rather
// than importing internal/config, exactly as feedback.Config does.
type Config struct {
	// Cmd is editor_cmd, with {file}, {repo} and {line} placeholders.
	Cmd string
	// Strategy is editor_strategy; empty means auto.
	Strategy string
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
	StrategyInline Strategy = "inline" // take over differ's terminal
	// StrategyCustom is an editor_cmd that is itself a tmux command. It is
	// not an editor invocation but a mechanism, so it runs as written instead
	// of being wrapped in one of ours.
	StrategyCustom Strategy = "custom"
)

// Kind says how the caller must run a plan.
type Kind int

const (
	// KindTerminal needs differ's terminal. The caller must run Argv in Dir
	// with tea.ExecProcess, so differ suspends and resumes around it.
	KindTerminal Kind = iota
)

// Plan is a decided course of action.
type Plan struct {
	Kind     Kind
	Strategy Strategy
	// Argv and Dir are set for KindTerminal.
	Argv []string
	Dir  string
	// Desc is the status line for a success. Empty means say nothing.
	Desc string
}

// Resolve picks a strategy and returns the plan. Errors carry text already fit
// for differ's status bar — the caller never reformats them.
func Resolve(_ context.Context, cfg Config, req Request) (Plan, error) {
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

	argv := buildArgv(cfg.Cmd, req)
	if _, err := exec.LookPath(argv[0]); err != nil {
		return Plan{}, fmt.Errorf("editor %q not found on PATH — set editor_cmd or $EDITOR", argv[0])
	}
	strategy := StrategyInline
	if filepath.Base(argv[0]) == "tmux" {
		strategy = StrategyCustom
	}
	return Plan{
		Kind:     KindTerminal,
		Strategy: strategy,
		Argv:     argv,
		Dir:      req.Repo,
	}, nil
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
