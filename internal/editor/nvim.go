package editor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Handing a file to an nvim that is already running.
//
// nvim listens on a socket by default. On Linux that is under
// $XDG_RUNTIME_DIR; on macOS that variable is unset, so nvim falls back to
// $TMPDIR/nvim.$USER/<random>/nvim.<pid>.0 — which is why socket discovery is
// the primary path here and not a fallback.
//
// Everything goes through --remote-expr, never --remote. With an unreachable
// server --remote prints "Editing locally" and starts a real nvim in the
// foreground; from the goroutine behind a tea.Cmd that would fight differ for
// the terminal. --remote-expr simply fails.

// probeTimeout bounds asking an nvim a question. One sitting on a hit-enter
// prompt accepts the connection and never answers, and must not wedge the UI.
const probeTimeout = 1 * time.Second

// nvimServer is a running nvim differ could hand a file to.
type nvimServer struct {
	Socket string
	Pane   string // $TMUX_PANE as that nvim inherited it
	CWD    string
}

// socketCandidateRoots are the directories nvim may have put its socket in.
//
// nvim uses stdpath('run'): $XDG_RUNTIME_DIR where one exists, else $TMPDIR,
// else /tmp. The last is the common case on a plain SSH session or in a
// container without systemd-logind, where neither variable is set — without
// it reuse would quietly never fire there.
func socketCandidateRoots(env Env) []string {
	var roots []string
	if env.XDGRuntimeDir != "" {
		roots = append(roots, strings.TrimSuffix(env.XDGRuntimeDir, "/"))
	}
	if env.TmpDir != "" {
		roots = append(roots, strings.TrimSuffix(env.TmpDir, "/"))
	}
	return append(roots, "/tmp")
}

// socketCandidates lists the sockets nvim may be listening on.
func socketCandidates(env Env) []string {
	roots := socketCandidateRoots(env)

	var out []string
	seen := map[string]bool{}
	for _, root := range roots {
		// nvim.<user>/<random>/nvim.<pid>.0
		matches, _ := filepath.Glob(filepath.Join(root, "nvim."+env.User, "*", "nvim.*"))
		// Older layouts put the socket directly under the root.
		direct, _ := filepath.Glob(filepath.Join(root, "nvim.*.0"))
		for _, m := range append(matches, direct...) {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

// queryNvim evaluates a vimscript expression in a running nvim.
func queryNvim(ctx context.Context, socket, expr string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "nvim", "--server", socket, "--remote-expr", expr).Output()
	if err != nil {
		// .Output() collects stderr into ExitError, but %w prints only
		// "exit status 1". nvim says why on stderr, so pass that on.
		return "", fmt.Errorf("nvim on %s did not answer: %w%s", socket, err, stderrOf(err))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// discoverNvim asks every reachable socket which pane it lives in.
func discoverNvim(ctx context.Context, env Env) []nvimServer {
	var found []nvimServer
	for _, sock := range socketCandidates(env) {
		out, err := queryNvim(ctx, sock, `$TMUX_PANE . "\n" . getcwd(-1,-1)`)
		if err != nil {
			continue
		}
		parts := strings.SplitN(out, "\n", 2)
		s := nvimServer{Socket: sock, Pane: strings.TrimSpace(parts[0])}
		if len(parts) > 1 {
			s.CWD = strings.TrimSpace(parts[1])
		}
		found = append(found, s)
	}
	return found
}

// openInNvim opens a file in a running nvim, at a line when one is known.
//
// :drop rather than :edit, and one call rather than an open followed by a
// cursor move. :drop reuses a window that already shows the file, and it
// never abandons a modified buffer: with nvim's default 'hidden' the old
// buffer simply goes hidden, and with 'nohidden' :drop opens a window instead
// of failing. Either way nothing unsaved is lost.
func openInNvim(ctx context.Context, socket, absPath string, line int) error {
	cmd := "drop "
	if line > 0 {
		cmd += "+" + strconv.Itoa(line) + " "
	}
	expr := fmt.Sprintf("execute('%s' . fnameescape('%s'))", cmd, vimEscape(absPath))

	out, err := queryNvim(ctx, socket, expr)
	if err != nil {
		return err
	}
	// execute() returns everything the command printed, which on a perfectly
	// successful open is the file announcement — `"/path" 3L, 6B` — whenever
	// 'shortmess' lacks F, or a plugin echoes on BufReadPost. Reporting that
	// as a failure also skipped focusing the pane, so the file opened and
	// nothing appeared to happen. Only an actual complaint counts; when there
	// is one, it is nvim's own words, never a bang retry.
	if msg := nvimComplaint(out); msg != "" {
		return fmt.Errorf("nvim: %s", msg)
	}
	return nil
}

// vimError matches the two shapes nvim uses to complain: a numbered error
// such as E37, and an autocommand failure.
var vimError = regexp.MustCompile(`\bE\d+:|(^|\n)\s*Error\b`)

// nvimComplaint returns the text of a complaint in execute() output, or "".
func nvimComplaint(out string) string {
	msg := strings.TrimSpace(out)
	if msg == "" || !vimError.MatchString(msg) {
		return ""
	}
	return strings.ReplaceAll(msg, "\n", " ")
}

// stderrOf renders the stderr an ExitError carried, ready to append.
func stderrOf(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if s := strings.TrimSpace(string(ee.Stderr)); s != "" {
			return ": " + strings.ReplaceAll(s, "\n", " ")
		}
	}
	return ""
}

// vimEscape quotes a path for a vimscript single-quoted string, where the
// only special character is the quote itself, doubled.
func vimEscape(s string) string { return strings.ReplaceAll(s, "'", "''") }

// reusePlan hands the file to an editor already open in differ's session.
//
// The session is resolved from differ's own pane, every candidate is checked
// against the live pane list, and the nvim that answers is matched to its
// pane by the $TMUX_PANE it inherited. A socket can outlive the pane it was
// created in, so the cross-check is what stops differ reporting that it
// opened a file somewhere invisible.
func reusePlan(ctx context.Context, req Request) (Plan, error) {
	session, err := currentSession(ctx, req.Env.TmuxPane)
	if err != nil {
		return Plan{}, err
	}
	all, err := listPanes(ctx)
	if err != nil {
		return Plan{}, err
	}

	self := pane{}
	byID := make(map[string]pane, len(all))
	for _, p := range all {
		byID[p.ID] = p
		if p.ID == req.Env.TmuxPane {
			self = p
		}
	}

	candidates := rankPanes(editorPanes(all, session), req.Repo, self)
	if len(candidates) == 0 {
		return Plan{}, fmt.Errorf(
			"no editor open in session %q — set editor_strategy to window", session)
	}

	servers := discoverNvim(ctx, req.Env)
	for _, c := range candidates {
		for _, s := range servers {
			if s.Pane != c.ID {
				continue
			}
			if _, live := byID[s.Pane]; !live {
				continue
			}
			target, socket, label := c.Target(), s.Socket, c.Label()
			return Plan{
				Kind:     KindDetached,
				Strategy: StrategyReuse,
				Desc:     "opened " + req.File + " in nvim (" + label + ")",
				run: func(ctx context.Context) error {
					if err := openInNvim(ctx, socket, req.abs(), req.Line); err != nil {
						return err
					}
					return focusPane(ctx, target)
				},
			}, nil
		}
	}
	return Plan{}, fmt.Errorf(
		"the editor in session %q does not answer on a socket — set editor_strategy to window", session)
}
