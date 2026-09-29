package editor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// repoWith builds a throwaway directory holding the named files, standing in
// for a git repo. The editor package never asks git anything — it only needs a
// root and a file that exists.
func repoWith(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, n := range names {
		p := filepath.Join(root, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// stubEditor writes an executable script and returns its absolute path.
//
// Tests name an editor by absolute path rather than relying on one being
// installed: exec.LookPath accepts an absolute path directly, so this needs no
// PATH juggling — and therefore no t.Setenv, which would bar t.Parallel.
func stubEditor(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolve_DefaultsToTheEditorFromTheEnvironment(t *testing.T) {
	t.Parallel()
	root := repoWith(t, "src.ts")
	ed := stubEditor(t, "myed")

	plan, err := Resolve(context.Background(), Config{}, Request{
		File: "src.ts",
		Repo: root,
		Env:  Env{Editor: ed},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := []string{ed, filepath.Join(root, "src.ts")}
	if !reflect.DeepEqual(plan.Argv, want) {
		t.Errorf("Argv = %q, want %q", plan.Argv, want)
	}
	if plan.Kind != KindTerminal {
		t.Errorf("Kind = %v, want KindTerminal", plan.Kind)
	}
	if plan.Strategy != StrategyInline {
		t.Errorf("Strategy = %q, want %q", plan.Strategy, StrategyInline)
	}
	if plan.Dir != root {
		t.Errorf("Dir = %q, want %q", plan.Dir, root)
	}
}

// The argv contract is checked without exec.LookPath, so these cases hold on
// any machine regardless of which editors happen to be installed.
func TestBuildArgv(t *testing.T) {
	t.Parallel()
	const root, abs = "/repo", "/repo/src.ts"

	cases := []struct {
		name   string
		tmpl   string
		editor string
		file   string
		line   int
		want   []string
	}{
		{
			name: "{line} expands",
			tmpl: "myed +{line} {file}",
			file: "src.ts",
			line: 42,
			want: []string{"myed", "+42", abs},
		},
		{
			// Never leave the literal and never emit an empty element: a
			// template like "subl {file}:{line}" must stay well formed even
			// from the file list, where there is no line.
			name: "{line} with no known line becomes 1",
			tmpl: "myed {file}:{line}",
			file: "src.ts",
			want: []string{"myed", abs + ":1"},
		},
		{
			name: "no editor anywhere falls back to vi",
			file: "src.ts",
			want: []string{"vi", abs},
		},
		{
			// $EDITOR is a command line, not a binary name — git reads it the
			// same way.
			name:   "an editor carrying arguments is split",
			editor: "code --wait",
			file:   "src.ts",
			want:   []string{"code", "--wait", abs},
		},
		{
			// The bug this package exists to fix: cmd/root.go expanded the
			// template and only then split it, so a path with a space became
			// two arguments.
			name:   "a path with spaces stays one argument",
			editor: "nvim",
			file:   "src/my component.tsx",
			want:   []string{"nvim", "/repo/src/my component.tsx"},
		},
		{
			// cmd/root.go indexed parts[0] after strings.Fields, so a
			// whitespace-only editor_cmd panicked. It now means "unset".
			name:   "a whitespace-only template means unset",
			tmpl:   "   ",
			editor: "nvim",
			file:   "src.ts",
			want:   []string{"nvim", abs},
		},
		{
			name:   "an explicit template beats the environment",
			tmpl:   "myed --flag {file}",
			editor: "nvim",
			file:   "src.ts",
			want:   []string{"myed", "--flag", abs},
		},
		{
			name: "{repo} expands, and a token holding both stays one argument",
			tmpl: "myed {repo}/.editorconfig {file}",
			file: "src.ts",
			want: []string{"myed", "/repo/.editorconfig", abs},
		},

		// Without an explicit {file} the layout is ours, so a known editor
		// family gets the cursor's line. Only families installed on a
		// developer machine here are claimed — guessing at emacs or helix
		// would be untested code.
		{
			name:   "the vi family takes +line",
			editor: "nvim",
			file:   "src.ts",
			line:   42,
			want:   []string{"nvim", "+42", abs},
		},
		{
			name:   "nano takes +line too",
			editor: "nano",
			file:   "src.ts",
			line:   7,
			want:   []string{"nano", "+7", abs},
		},
		{
			name:   "the family is the basename, not the whole path",
			editor: "/opt/homebrew/bin/nvim",
			file:   "src.ts",
			line:   42,
			want:   []string{"/opt/homebrew/bin/nvim", "+42", abs},
		},
		{
			name:   "code takes --goto file:line",
			editor: "code --wait",
			file:   "src.ts",
			line:   42,
			want:   []string{"code", "--wait", "--goto", abs + ":42"},
		},
		{
			name:   "an unknown editor is given the file alone",
			editor: "myed",
			file:   "src.ts",
			line:   42,
			want:   []string{"myed", abs},
		},
		{
			name:   "no line known means no line argument",
			editor: "nvim",
			file:   "src.ts",
			want:   []string{"nvim", abs},
		},
		{
			// Once the template places {file} itself, its author owns the
			// layout — we have nowhere safe to insert, so they use {line}.
			name:   "a template that places {file} gets no injected line",
			tmpl:   "nvim -u x.vim {file}",
			editor: "nvim",
			file:   "src.ts",
			line:   42,
			want:   []string{"nvim", "-u", "x.vim", abs},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := buildArgv(c.tmpl, Request{
				Line: c.line,
				File: c.file, Repo: root, Env: Env{Editor: c.editor},
			})
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("buildArgv = %q, want %q", got, c.want)
			}
		})
	}
}

// The failure has to arrive before the TUI is torn down, naming the binary.
func TestResolve_AnEditorThatIsNotOnPathIsAnActionableError(t *testing.T) {
	t.Parallel()
	root := repoWith(t, "src.ts")

	_, err := Resolve(context.Background(), Config{}, Request{
		File: "src.ts", Repo: root, Env: Env{Editor: "definitely-not-a-real-binary"},
	})
	if err == nil {
		t.Fatal("want an error for an editor that is not installed")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-real-binary") {
		t.Errorf("error %q should name the binary", err)
	}
	if !strings.Contains(err.Error(), "editor_cmd") {
		t.Errorf("error %q should say how to fix it", err)
	}
}

// A D-status file is in the diff but not on disk. Opening it would give an
// empty buffer, and :w would resurrect the file empty. The os.Stat also
// catches the race where the agent removes the file between differ's 2 s poll
// and the key press.
func TestResolve_RefusesAFileThatIsNotOnDisk(t *testing.T) {
	t.Parallel()
	root := repoWith(t)
	ed := stubEditor(t, "myed")

	_, err := Resolve(context.Background(), Config{}, Request{
		File: "deleted.ts", Repo: root, Env: Env{Editor: ed},
	})
	if err == nil {
		t.Fatal("want an error for a file that is no longer on disk")
	}
	if !strings.Contains(err.Error(), "deleted.ts") {
		t.Errorf("error %q should name the file", err)
	}
}

// README's own sample config sets editor_cmd to a tmux command. Such a
// template is not an editor invocation, it is a mechanism, so it runs verbatim
// rather than being wrapped in another one.
func TestResolve_ATmuxEditorCmdRunsVerbatim(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	root := repoWith(t, "src.ts")

	plan, err := Resolve(context.Background(), Config{
		Cmd: "tmux new-window -c {repo} nvim {file}",
	}, Request{File: "src.ts", Repo: root, Env: Env{Editor: "nvim", InTmux: true}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if plan.Strategy != StrategyCustom {
		t.Errorf("Strategy = %q, want %q", plan.Strategy, StrategyCustom)
	}
	want := []string{"tmux", "new-window", "-c", root, "nvim", filepath.Join(root, "src.ts")}
	if !reflect.DeepEqual(plan.Argv, want) {
		t.Errorf("Argv = %q, want %q", plan.Argv, want)
	}
}

func TestResolve_StrategySelection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		strategy string
		inTmux   bool
		want     Strategy
		wantErr  string
	}{
		{name: "auto outside tmux is inline", want: StrategyInline},
		{name: "explicit inline", strategy: "inline", want: StrategyInline},
		{name: "explicit inline wins even inside tmux", strategy: "inline", inTmux: true, want: StrategyInline},
		{name: "window inside tmux", strategy: "window", inTmux: true, want: StrategyWindow},
		{name: "auto inside tmux with nothing to reuse falls to window", inTmux: true, want: StrategyWindow},
		{
			// An explicit strategy that cannot be honoured is an error, never
			// a silent downgrade — the same choice feedback's tmux target
			// makes.
			name:     "window outside tmux is an actionable error",
			strategy: "window", wantErr: "not running inside tmux",
		},
		{name: "reuse outside tmux is an actionable error", strategy: "reuse", wantErr: "not running inside tmux"},
		{name: "an unknown strategy lists the valid ones", strategy: "sideways", wantErr: "editor_strategy"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := repoWith(t, "src.ts")
			ed := stubEditor(t, "myed")

			plan, err := Resolve(context.Background(),
				Config{Strategy: c.strategy},
				Request{File: "src.ts", Repo: root, Env: Env{Editor: ed, InTmux: c.inTmux, TmuxPane: "%1"}})

			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("want an error containing %q", c.wantErr)
				}
				if !strings.Contains(err.Error(), c.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if plan.Strategy != c.want {
				t.Errorf("Strategy = %q, want %q", plan.Strategy, c.want)
			}
		})
	}
}

func TestStrategies_AreListedForTheUser(t *testing.T) {
	t.Parallel()
	got := strings.Join(Strategies(), " ")
	for _, want := range []string{"auto", "reuse", "window", "inline"} {
		if !strings.Contains(got, want) {
			t.Errorf("Strategies() = %q, missing %q", got, want)
		}
	}
}

// A GUI editor reuses its own window and wants neither differ's terminal nor
// a tmux window. Nothing infers that — the user says so.
func TestResolve_DetachRunsTheEditorInTheBackground(t *testing.T) {
	t.Parallel()
	root := repoWith(t, "src.ts")
	ed := stubEditor(t, "myed")

	for _, inTmux := range []bool{false, true} {
		plan, err := Resolve(context.Background(), Config{Strategy: "detach", Cmd: ed + " {file}"},
			Request{File: "src.ts", Repo: root, Env: Env{Editor: ed, InTmux: inTmux, TmuxPane: "%1"}})
		if err != nil {
			t.Fatalf("InTmux=%v: %v", inTmux, err)
		}
		if plan.Strategy != StrategyDetach {
			t.Errorf("InTmux=%v: Strategy = %q, want detach", inTmux, plan.Strategy)
		}
		if plan.Kind != KindDetached {
			t.Errorf("InTmux=%v: Kind = %v, want KindDetached", inTmux, plan.Kind)
		}
	}
}

// It has to actually run the thing, and report a failure rather than swallow
// it.
func TestResolve_DetachActuallyRunsTheCommand(t *testing.T) {
	t.Parallel()
	root := repoWith(t, "src.ts")
	marker := filepath.Join(t.TempDir(), "ran.txt")
	ed := filepath.Join(t.TempDir(), "writer")
	script := "#!/bin/sh\nprintf '%s' \"$1\" > " + marker + "\n"
	if err := os.WriteFile(ed, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	plan, err := Resolve(context.Background(), Config{Strategy: "detach", Cmd: ed + " {file}"},
		Request{File: "src.ts", Repo: root, Env: Env{Editor: ed}})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("the editor did not run: %v", err)
	}
	if want := filepath.Join(root, "src.ts"); string(got) != want {
		t.Errorf("editor got %q, want %q", got, want)
	}
}

func TestResolve_DetachReportsAFailingEditor(t *testing.T) {
	t.Parallel()
	root := repoWith(t, "src.ts")
	ed := filepath.Join(t.TempDir(), "failer")
	if err := os.WriteFile(ed, []byte("#!/bin/sh\necho nope >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	plan, err := Resolve(context.Background(), Config{Strategy: "detach"},
		Request{File: "src.ts", Repo: root, Env: Env{Editor: ed}})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Run(context.Background()); err == nil {
		t.Error("want an error from an editor that exits non-zero")
	}
}
