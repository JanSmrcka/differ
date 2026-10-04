package editor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Opening a file inside herdr.
//
// herdr is a multiplexer built for coding agents, and it can be asked what
// tmux makes differ reconstruct: `pane list` and `workspace list` describe
// every pane and the repository each workspace is on. The listings print one
// line of JSON, `{"id":…,"result":{…}}`, and take no --json flag.
//
// Which pane an nvim is in comes from the nvim, not from herdr: herdr exports
// HERDR_PANE_ID into every pane exactly as tmux exports TMUX_PANE, and
// discoverNvim asks for both.

// herdrPane is one herdr pane as differ cares about it.
type herdrPane struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Cwd         string `json:"cwd"`
}

// herdrWorkspace is one herdr workspace. Worktree is absent for a workspace
// that is not on a git checkout.
type herdrWorkspace struct {
	ID       string `json:"workspace_id"`
	Label    string `json:"label"`
	Number   int    `json:"number"`
	Worktree *struct {
		// RepoKey is the repository's shared git directory — the same for
		// every linked worktree of it.
		RepoKey string `json:"repo_key"`
	} `json:"worktree"`
}

func (w herdrWorkspace) repoKey() string {
	if w.Worktree == nil {
		return ""
	}
	return w.Worktree.RepoKey
}

func parseHerdrPanes(out []byte) ([]herdrPane, error) {
	var body struct {
		Result struct {
			Panes []herdrPane `json:"panes"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return nil, fmt.Errorf("herdr pane list: %w", err)
	}
	return body.Result.Panes, nil
}

func parseHerdrWorkspaces(out []byte) ([]herdrWorkspace, error) {
	var body struct {
		Result struct {
			Workspaces []herdrWorkspace `json:"workspaces"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return nil, fmt.Errorf("herdr workspace list: %w", err)
	}
	return body.Result.Workspaces, nil
}

// herdrCandidate is a running nvim differ could hand the file to.
type herdrCandidate struct {
	Pane      herdrPane
	Socket    string
	Workspace string // the workspace's label, for the status line
}

// herdrCandidates joins the nvims that answered to the panes herdr lists,
// keeps those reuse may reach, and orders them best first. Pure, so the whole
// decision is tested without a server.
//
// The default scope is differ's own workspace and nothing else. An nvim on the
// same repository in another workspace — the main checkout's, seen from a
// worktree — is the same files, but focusing it pulls the user out of the
// workspace they are working in; a new pane beside differ does not. "any" and
// a named workspace reach further, and there the same repository still ranks
// first.
//
// A pane is matched by the HERDR_PANE_ID its nvim inherited, and only if herdr
// still lists it: a pane moved to another workspace gets a new id while the
// nvim keeps the old one, and opening there would claim success somewhere the
// user cannot see.
func herdrCandidates(panes []herdrPane, workspaces []herdrWorkspace, servers []nvimServer, req Request, target string) []herdrCandidate {
	byPane := make(map[string]herdrPane, len(panes))
	for _, p := range panes {
		byPane[p.PaneID] = p
	}
	byWS := make(map[string]herdrWorkspace, len(workspaces))
	for _, w := range workspaces {
		byWS[w.ID] = w
	}
	self := byPane[req.Env.HerdrPane]
	selfWS := req.Env.HerdrWorkspace
	if selfWS == "" {
		selfWS = self.WorkspaceID
	}
	ownKey := byWS[selfWS].repoKey()

	// tier is 0 for differ's workspace, 1 for the same repository, 2 for
	// anything else.
	tier := func(p herdrPane, s nvimServer) int {
		switch {
		case p.WorkspaceID == selfWS:
			return 0
		case ownKey != "" && byWS[p.WorkspaceID].repoKey() == ownKey, under(s.CWD, req.Repo):
			return 1
		}
		return 2
	}
	inScope := func(p herdrPane, s nvimServer) bool {
		switch target {
		case "", "session", "workspace":
			return p.WorkspaceID == selfWS
		case "any":
			return true
		}
		w := byWS[p.WorkspaceID]
		return w.Label == target || w.ID == target
	}

	type ranked struct {
		herdrCandidate
		tier, number int
		sameTab      bool
	}
	var found []ranked
	for _, s := range servers {
		p, live := byPane[s.HerdrPane]
		if s.HerdrPane == "" || !live || p.PaneID == req.Env.HerdrPane || !inScope(p, s) {
			continue
		}
		w := byWS[p.WorkspaceID]
		found = append(found, ranked{
			herdrCandidate: herdrCandidate{Pane: p, Socket: s.Socket, Workspace: w.Label},
			tier:           tier(p, s),
			number:         workspaceNumber(w),
			sameTab:        self.TabID != "" && p.TabID == self.TabID,
		})
	}
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		if a.sameTab != b.sameTab {
			return a.sameTab
		}
		return a.number < b.number
	})

	out := make([]herdrCandidate, len(found))
	for i, f := range found {
		out[i] = f.herdrCandidate
	}
	return out
}

// workspaceNumber is the workspace's position, so w2 sorts before w10.
// herdr reports it; the id is read only when it does not.
func workspaceNumber(w herdrWorkspace) int {
	if w.Number > 0 {
		return w.Number
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(w.ID, "w")); err == nil {
		return n
	}
	return 1 << 30
}

// herdrCall makes one request on herdr's API socket.
//
// Everything else goes through the herdr CLI, as git does. This exists for
// pane.focus alone: the API has it, and `herdr pane focus` only moves to a
// neighbour. The protocol is one JSON line each way, with failures as
// {"error":{"code","message"}}.
func herdrCall(ctx context.Context, timeout time.Duration, socket, method string, params any) error {
	if socket == "" {
		return errors.New("herdr did not say where its socket is (HERDR_SOCKET_PATH)")
	}
	if timeout <= 0 {
		timeout = actTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return fmt.Errorf("herdr %s: %w", method, err)
	}
	defer func() { _ = conn.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	req, err := json.Marshal(map[string]any{"id": "differ", "method": method, "params": params})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return fmt.Errorf("herdr %s: %w", method, err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("herdr %s did not answer: %w", method, err)
	}
	var resp struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("herdr %s: %w", method, err)
	}
	if resp.Error != nil {
		return fmt.Errorf("herdr: %s (%s)", resp.Error.Message, resp.Error.Code)
	}
	return nil
}

// herdrReusePlan hands the file to an nvim already open in herdr.
//
// The listings and the nvim sockets are asked at once; together they cost
// the slowest of them. Each candidate is then checked against what herdr
// says is in its foreground — an nvim suspended with ctrl-z still answers on
// its socket while its pane shows a shell.
func herdrReusePlan(ctx context.Context, cfg Config, req Request) (Plan, error) {
	// Without the socket the pane cannot be focused. Finding that out after
	// :drop would report a failure for a file that did open, on every press.
	if req.Env.HerdrSocket == "" {
		return Plan{}, errors.New("herdr did not say where its socket is (HERDR_SOCKET_PATH) — set editor_strategy to window")
	}
	bin := herdrBinOf(req.Env)
	type answer struct {
		out []byte
		err error
	}
	ask := func(args ...string) <-chan answer {
		ch := make(chan answer, 1)
		go func() {
			out, err := runHerdr(ctx, cfg.act(), bin, args...)
			ch <- answer{out, err}
		}()
		return ch
	}
	panesCh, wsCh := ask("pane", "list"), ask("workspace", "list")
	servers := discoverNvim(ctx, cfg.probe(), req.Env)
	pa, wa := <-panesCh, <-wsCh
	if pa.err != nil {
		return Plan{}, pa.err
	}
	if wa.err != nil {
		return Plan{}, wa.err
	}
	panes, err := parseHerdrPanes(pa.out)
	if err != nil {
		return Plan{}, err
	}
	workspaces, err := parseHerdrWorkspaces(wa.out)
	if err != nil {
		return Plan{}, err
	}

	for _, c := range herdrCandidates(panes, workspaces, servers, req, cfg.Target) {
		if !herdrRunsEditor(ctx, cfg, bin, c.Pane.PaneID) {
			continue
		}
		return herdrOpenPlan(cfg, req, c), nil
	}
	return Plan{}, fmt.Errorf(
		"no editor open in %s — set editor_strategy to window", herdrScopeLabel(cfg.Target))
}

// herdrOpenPlan opens the file in the candidate's nvim and brings its pane to
// the front.
func herdrOpenPlan(cfg Config, req Request, c herdrCandidate) Plan {
	label := c.Pane.PaneID
	if c.Workspace != "" {
		label = c.Workspace + " " + label
	}
	return Plan{
		Kind:     KindDetached,
		Strategy: StrategyReuse,
		Desc:     "opened " + req.File + " in nvim (" + label + ")",
		run: func(ctx context.Context) error {
			if err := openInNvim(ctx, cfg.act(), c.Socket, req.abs(), req.Line); err != nil {
				return err
			}
			if err := herdrCall(ctx, cfg.act(), req.Env.HerdrSocket, "pane.focus",
				map[string]string{"pane_id": c.Pane.PaneID}); err != nil {
				return fmt.Errorf("opened %s in %s, but could not bring it to the front: %w",
					req.File, label, err)
			}
			return nil
		},
	}
}

// herdrRunsEditor reports whether the pane's foreground is something
// editor_panes names. A failed question counts as no: the next candidate, or
// a new tab, is better than opening the file out of sight.
func herdrRunsEditor(ctx context.Context, cfg Config, bin, pane string) bool {
	out, err := runHerdr(ctx, cfg.act(), bin, "pane", "process-info", "--pane", pane)
	if err != nil {
		return false
	}
	var body struct {
		Result struct {
			Info struct {
				Foreground []struct {
					Name string `json:"name"`
				} `json:"foreground_processes"`
			} `json:"process_info"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &body) != nil {
		return false
	}
	allow := cfg.Panes
	if len(allow) == 0 {
		allow = defaultEditorPanes
	}
	for _, p := range body.Result.Info.Foreground {
		for _, a := range allow {
			if p.Name == strings.TrimSpace(a) {
				return true
			}
		}
	}
	return false
}

// herdrScopeLabel names where reuse looked, for an error the user can act on.
func herdrScopeLabel(target string) string {
	switch target {
	case "", "session", "workspace":
		return "this workspace"
	case "any":
		return "any workspace"
	}
	return "workspace " + strconv.Quote(target)
}

// herdrBinOf is the herdr executable: the one herdr says it is, so a herdr
// that is not on PATH still works.
func herdrBinOf(env Env) string {
	if env.HerdrBin != "" {
		return env.HerdrBin
	}
	return "herdr"
}

// runHerdr runs one herdr command and returns its stdout, or herdr's own
// error from stderr.
//
// Duplicated from internal/feedback/herdr.go for the reason tmux.go gives:
// the two want different advice in their errors. Extract to internal/herdr
// if a third consumer appears.
func runHerdr(ctx context.Context, timeout time.Duration, bin string, args ...string) ([]byte, error) {
	if timeout <= 0 {
		timeout = actTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("herdr %s: %w", strings.Join(args, " "), ctx.Err())
		}
		return nil, parseHerdrError(args, stderr.Bytes(), err)
	}
	return stdout.Bytes(), nil
}

// parseHerdrError reads herdr's JSON error from stderr, falling back to the
// raw text, then to the process error.
func parseHerdrError(args []string, stderr []byte, err error) error {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(stderr, &body) == nil && body.Error.Code != "" {
		return fmt.Errorf("herdr: %s (%s)", body.Error.Message, body.Error.Code)
	}
	if said := strings.TrimSpace(string(stderr)); said != "" {
		return fmt.Errorf("herdr %s: %s", strings.Join(args, " "), strings.ReplaceAll(said, "\n", " "))
	}
	return fmt.Errorf("herdr %s: %w", strings.Join(args, " "), err)
}

// herdrPanePlan opens the editor in a new pane beside differ's — herdr's
// equivalent of a tmux window, but in differ's own tab, so the user is never
// taken out of the workspace they are in. Focused, because the user pressed
// e and wants to be there.
//
// herdr cannot start a pane on a command, only on a shell, so the command is
// typed into it with `pane run`. It is exec'd, so the pane closes when the
// editor quits instead of leaving a shell behind, and quoted for the shell
// that pane actually runs: fish reads a quote inside single quotes as \',
// where a POSIX shell needs '\”.
func herdrPanePlan(cfg Config, argv []string, req Request) Plan {
	return Plan{
		Kind:     KindDetached,
		Strategy: StrategyWindow,
		Argv:     argv,
		Dir:      req.Repo,
		Desc:     "opened " + req.File + " in a new pane",
		run: func(ctx context.Context) error {
			bin := herdrBinOf(req.Env)
			self := req.Env.HerdrPane
			dir := splitDirection(herdrPaneSize(ctx, cfg, bin, self))
			out, err := runHerdr(ctx, cfg.act(), bin, "pane", "split", "--pane", self,
				"--direction", dir, "--cwd", req.Repo, "--focus")
			if err != nil {
				return err
			}
			var body struct {
				Result struct {
					Pane herdrPane `json:"pane"`
				} `json:"result"`
			}
			if err := json.Unmarshal(out, &body); err != nil || body.Result.Pane.PaneID == "" {
				return fmt.Errorf("herdr pane split did not say which pane it made")
			}
			pane := body.Result.Pane.PaneID
			quote := shellQuote
			if paneShell(ctx, cfg, bin, pane, req.Env.Shell) == "fish" {
				quote = fishQuote
			}
			if _, err := runHerdr(ctx, cfg.act(), bin, "pane", "run", pane, "exec "+quote(argv)); err != nil {
				// --focus already moved the user there; an empty shell away
				// from differ is no place to leave them with the error.
				_, _ = runHerdr(ctx, cfg.act(), bin, "pane", "close", pane)
				return err
			}
			return nil
		},
	}
}

// splitDirection splits a wide pane side by side and a narrow one top and
// bottom. A cell is about twice as tall as it is wide, so a pane is wide when
// its width is at least twice its height. Unknown size splits right.
func splitDirection(width, height int) string {
	if height > 0 && width < 2*height {
		return "down"
	}
	return "right"
}

// herdrPaneSize is the pane's size in cells, or zeros when herdr will not say.
func herdrPaneSize(ctx context.Context, cfg Config, bin, pane string) (int, int) {
	out, err := runHerdr(ctx, cfg.act(), bin, "pane", "layout", "--pane", pane)
	if err != nil {
		return 0, 0
	}
	var body struct {
		Result struct {
			Layout struct {
				Panes []struct {
					ID   string `json:"pane_id"`
					Rect struct {
						Width  int `json:"width"`
						Height int `json:"height"`
					} `json:"rect"`
				} `json:"panes"`
			} `json:"layout"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &body) != nil {
		return 0, 0
	}
	for _, p := range body.Result.Layout.Panes {
		if p.ID == pane {
			return p.Rect.Width, p.Rect.Height
		}
	}
	return 0, 0
}

// shellPollEvery and shellPolls bound waiting for a new pane's shell to
// appear in process-info: straight after the split it may not have started.
const (
	shellPollEvery = 50 * time.Millisecond
	shellPolls     = 10
)

// paneShell names the shell a new pane runs, as a bare name: "fish", not
// "-fish" (a login shell) or a path. It asks herdr, waiting briefly for the
// shell to start, and otherwise assumes $SHELL — herdr's own default when
// default_shell is unset.
func paneShell(ctx context.Context, cfg Config, bin, pane, fallback string) string {
	name := ""
	for i := 0; i < shellPolls && name == ""; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ""
			case <-time.After(shellPollEvery):
			}
		}
		name = herdrShell(ctx, cfg, bin, pane)
		// Between fork and exec the shell's process is still herdr's.
		if filepath.Base(name) == filepath.Base(bin) {
			name = ""
		}
	}
	if name == "" {
		name = fallback
	}
	return filepath.Base(strings.TrimPrefix(name, "-"))
}

// herdrShell is the name of the process herdr calls the pane's shell, or ""
// when it is not running yet or herdr will not say.
func herdrShell(ctx context.Context, cfg Config, bin, pane string) string {
	out, err := runHerdr(ctx, cfg.act(), bin, "pane", "process-info", "--pane", pane)
	if err != nil {
		return ""
	}
	var body struct {
		Result struct {
			Info struct {
				ShellPID   int `json:"shell_pid"`
				Foreground []struct {
					Name string `json:"name"`
					PID  int    `json:"pid"`
				} `json:"foreground_processes"`
			} `json:"process_info"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &body) != nil {
		return ""
	}
	for _, p := range body.Result.Info.Foreground {
		if p.PID == body.Result.Info.ShellPID {
			return p.Name
		}
	}
	return ""
}

// shellQuote renders argv as one POSIX shell command line: every argument in
// single quotes, where nothing is special but the quote itself.
func shellQuote(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

// fishQuote is shellQuote for fish, whose single quotes take two escapes —
// \' and \\ — and treat every other backslash literally.
func fishQuote(argv []string) string {
	esc := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = "'" + esc.Replace(a) + "'"
	}
	return strings.Join(quoted, " ")
}
