package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The CLI is exercised by building the binary once and running it, because
// what matters is what a user sees: the text on stderr, the exit code, and
// whether the usage block is dumped over a runtime failure.
var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

// binary builds differ once for the whole package. The tests run in parallel,
// so this cannot be a plain lazy assignment.
func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "differcli")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(dir, "differ")
		build := exec.Command("go", "build", "-o", binPath, ".")
		build.Dir = ".."
		if out, err := build.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("build: %w\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return binPath
}

type result struct {
	stdout, stderr string
	code           int
}

func runCLI(t *testing.T, dir string, args ...string) result {
	t.Helper()
	cmd := exec.Command(binary(t), args...)
	cmd.Dir = dir
	// A scrubbed environment: NO_COLOR in the developer's shell would change
	// what differ does, and the real HOME would have it read the developer's
	// own config file.
	cmd.Env = append(os.Environ(), "NO_COLOR=", "HOME="+t.TempDir())
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()

	code := 0
	var ee *exec.ExitError
	if err != nil {
		if ok := asExitError(err, &ee); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run: %v", err)
		}
	}
	return result{stdout: out.String(), stderr: errb.String(), code: code}
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// notARepo is a directory git knows nothing about.
func notARepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	// Guard against the temp dir sitting inside someone's repository.
	if err := os.WriteFile(filepath.Join(d, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

// A runtime failure used to print the whole usage block, which buries the one
// line that says what went wrong.
func TestCLI_ARuntimeErrorIsOneLineWithNoUsage(t *testing.T) {
	t.Parallel()
	got := runCLI(t, notARepo(t), "--ref", "definitely-not-a-ref")

	if got.code == 0 {
		t.Error("a runtime failure should exit non-zero")
	}
	if strings.Contains(got.stderr, "Usage:") || strings.Contains(got.stdout, "Usage:") {
		t.Errorf("usage was dumped over a runtime error:\n%s%s", got.stdout, got.stderr)
	}
	if lines := strings.Count(strings.TrimSpace(got.stderr), "\n"); lines > 2 {
		t.Errorf("error is %d lines, want a concise one:\n%s", lines+1, got.stderr)
	}
}

// A misspelled flag is a usage error, and there the usage block is the point.
func TestCLI_AUsageErrorStillShowsUsage(t *testing.T) {
	t.Parallel()
	got := runCLI(t, notARepo(t), "--no-such-flag")

	if got.code == 0 {
		t.Error("an unknown flag should exit non-zero")
	}
	if !strings.Contains(got.stderr, "Usage:") {
		t.Errorf("a usage error should show usage:\n%s", got.stderr)
	}
}

// Exit codes have to be predictable enough to script against.
func TestCLI_ExitCodesDistinguishUsageFromRuntime(t *testing.T) {
	t.Parallel()
	usage := runCLI(t, notARepo(t), "--no-such-flag")
	runtime := runCLI(t, notARepo(t), "--ref", "definitely-not-a-ref")

	if usage.code != 2 {
		t.Errorf("usage error exited %d, want 2", usage.code)
	}
	if runtime.code != 1 {
		t.Errorf("runtime error exited %d, want 1", runtime.code)
	}
}

// --version and --help are the two things every CLI is tried with first.
func TestCLI_VersionAndHelpSucceed(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--version"}, {"--help"}, {"review", "--help"}} {
		got := runCLI(t, notARepo(t), args...)
		if got.code != 0 {
			t.Errorf("%v exited %d:\n%s", args, got.code, got.stderr)
		}
		if strings.TrimSpace(got.stdout) == "" {
			t.Errorf("%v printed nothing", args)
		}
	}
}

// Help that does not show how to use the thing is not help.
func TestCLI_HelpCarriesExamples(t *testing.T) {
	t.Parallel()
	got := runCLI(t, notARepo(t), "--help")

	if !strings.Contains(got.stdout, "Examples:") {
		t.Errorf("--help has no examples:\n%s", got.stdout)
	}
	for _, want := range []string{"differ -s", "differ -r main", "differ review"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("--help does not show %q:\n%s", want, got.stdout)
		}
	}
}

// review is a first-class entry point, with the same comparison flags as the
// root command.
func TestCLI_ReviewIsListedWithTheSameFlags(t *testing.T) {
	t.Parallel()
	root := runCLI(t, notARepo(t), "--help")
	if !strings.Contains(root.stdout, "review") {
		t.Errorf("review is not listed in --help:\n%s", root.stdout)
	}

	got := runCLI(t, notARepo(t), "review", "--help")
	for _, want := range []string{"--staged", "--ref", "--no-color"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("differ review --help is missing %q:\n%s", want, got.stdout)
		}
	}
}

// Nothing may reach stdout before the TUI takes the screen, or it scrolls the
// alt screen and leaves the terminal in a mess.
func TestCLI_NothingIsPrintedBeforeTheTUI(t *testing.T) {
	t.Parallel()
	// With no TTY the program cannot start, so the only stdout here would be
	// something printed on the way in.
	got := runCLI(t, notARepo(t))
	if strings.TrimSpace(got.stdout) != "" {
		t.Errorf("stdout before the TUI: %q", got.stdout)
	}
}

// A flag error on a subcommand must show that subcommand's usage. It used to
// print the root's, including flags the subcommand does not accept.
func TestCLI_ASubcommandsFlagErrorShowsItsOwnUsage(t *testing.T) {
	t.Parallel()
	got := runCLI(t, notARepo(t), "log", "--bogus")

	if got.code != 2 {
		t.Errorf("exited %d, want 2", got.code)
	}
	if !strings.Contains(got.stderr, "differ log") {
		t.Errorf("usage is not the subcommand's:\n%s", got.stderr)
	}
	for _, unwanted := range []string{"--commit", "--staged"} {
		if strings.Contains(got.stderr, unwanted) {
			t.Errorf("usage offers %q, which log does not accept:\n%s", unwanted, got.stderr)
		}
	}
}

// An unknown subcommand is a bad command line, so it exits 2 like any other.
func TestCLI_AnUnknownSubcommandIsAUsageError(t *testing.T) {
	t.Parallel()
	got := runCLI(t, notARepo(t), "lgo")

	if got.code != 2 {
		t.Errorf("exited %d, want 2 for an unknown command", got.code)
	}
	if !strings.Contains(got.stderr, "lgo") {
		t.Errorf("the error does not name the command:\n%s", got.stderr)
	}
	if !strings.Contains(got.stderr, "Usage:") {
		t.Errorf("an unknown command should show usage:\n%s", got.stderr)
	}
}

// An unknown --theme used to fall back to dark without a word, so a typo left
// you wondering why the colours had not changed.
func TestCLI_AnUnknownThemeIsAUsageError(t *testing.T) {
	t.Parallel()
	got := runCLI(t, notARepo(t), "--theme", "solarised")

	if got.code != 2 {
		t.Errorf("exited %d, want 2 for a bad flag value", got.code)
	}
	if !strings.Contains(got.stderr, "solarised") {
		t.Errorf("the error does not name the theme:\n%s", got.stderr)
	}
	// And it says what the choices are, because the user cannot guess them.
	for _, want := range []string{"mocha", "gruvbox", "tokyonight"} {
		if !strings.Contains(got.stderr, want) {
			t.Errorf("the error does not offer %q:\n%s", want, got.stderr)
		}
	}
}

// The themes are listed where someone would look for them.
func TestCLI_HelpListsTheThemes(t *testing.T) {
	t.Parallel()
	got := runCLI(t, notARepo(t), "--help")

	// On the --theme line specifically. Somewhere in the help text would also
	// be satisfied by a passing mention in the examples.
	var line string
	for _, l := range strings.Split(got.stdout, "\n") {
		if strings.Contains(l, "--theme") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("--help has no --theme line:\n%s", got.stdout)
	}
	for _, want := range []string{"mocha", "latte", "gruvbox", "tokyonight", "github"} {
		if !strings.Contains(line, want) {
			t.Errorf("the --theme line does not offer %q: %q", want, line)
		}
	}
}

// A bad theme name is a bad theme name whatever else is on the command line.
// It used to be accepted whenever colour was off, because the no-colour
// short-circuit returned before the name was looked at.
func TestCLI_ABadThemeIsRefusedEvenWithColourOff(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--no-color", "--theme", "solarised"},
		{"--theme", "solarised", "--no-color"},
	} {
		got := runCLI(t, notARepo(t), args...)
		if got.code != 2 {
			t.Errorf("%v exited %d, want 2:\n%s", args, got.code, got.stderr)
		}
		if !strings.Contains(got.stderr, "solarised") {
			t.Errorf("%v: the error does not name the theme:\n%s", args, got.stderr)
		}
	}

	// And with NO_COLOR in the environment.
	cmd := exec.Command(binary(t), "--theme", "solarised")
	cmd.Dir = notARepo(t)
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "HOME="+t.TempDir())
	var errb bytes.Buffer
	cmd.Stderr = &errb
	err := cmd.Run()

	var ee *exec.ExitError
	if !asExitError(err, &ee) || ee.ExitCode() != 2 {
		t.Errorf("with NO_COLOR set, exited %v, want 2:\n%s", err, errb.String())
	}
	if !strings.Contains(errb.String(), "solarised") {
		t.Errorf("with NO_COLOR set, the error does not name the theme:\n%s", errb.String())
	}
}
