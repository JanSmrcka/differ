package editor

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// herdrFixtures parses the captured `pane list` and `workspace list` output.
// The shape is herdr 0.9.3's, with the paths replaced.
func herdrFixtures(t *testing.T) ([]herdrPane, []herdrWorkspace) {
	t.Helper()
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	panes, err := parseHerdrPanes(read("herdr_pane_list.json"))
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := parseHerdrWorkspaces(read("herdr_workspace_list.json"))
	if err != nil {
		t.Fatal(err)
	}
	return panes, workspaces
}

func TestHerdrCandidates(t *testing.T) {
	t.Parallel()
	panes, workspaces := herdrFixtures(t)
	nvim := func(pane, cwd string) nvimServer {
		return nvimServer{Socket: "/s/" + pane, HerdrPane: pane, CWD: cwd}
	}
	// differ in the linked worktree w3, whose repository is also checked out
	// in w2 and has nothing to do with w1.
	inWorktree := Request{Repo: "/wt/brave", Env: Env{HerdrPane: "w3:p1", HerdrWorkspace: "w3"}}
	// differ in a workspace herdr knows no repository for.
	inNotes := Request{Repo: "/notes", Env: Env{HerdrPane: "w4:p1", HerdrWorkspace: "w4"}}

	cases := []struct {
		name    string
		req     Request
		target  string
		servers []nvimServer
		want    []string
	}{
		{
			name: "own workspace only, its own tab first",
			req:  inWorktree,
			servers: []nvimServer{
				nvim("w2:p1", "/src/differ"), nvim("w3:p2", "/wt/brave"), nvim("w3:p3", "/wt/brave"),
			},
			want: []string{"w3:p3", "w3:p2"},
		},
		{
			// The main checkout's nvim is the same files, but reaching it
			// would pull the user out of the workspace they are in.
			name:    "another workspace is never reached by default, even on the same repository",
			req:     inWorktree,
			servers: []nvimServer{nvim("w1:p1", "/src/api"), nvim("w2:p1", "/src/differ")},
		},
		{
			name:    "any reaches every workspace, by workspace number",
			req:     inWorktree,
			target:  "any",
			servers: []nvimServer{nvim("w10:p1", "/scratch"), nvim("w1:p1", "/src/api"), nvim("w2:p1", "/src/differ")},
			want:    []string{"w2:p1", "w1:p1", "w10:p1"},
		},
		{
			name:    "a workspace named by label",
			req:     inWorktree,
			target:  "api",
			servers: []nvimServer{nvim("w1:p1", "/src/api"), nvim("w2:p1", "/src/differ")},
			want:    []string{"w1:p1"},
		},
		{
			name:    "a workspace named by id",
			req:     inWorktree,
			target:  "w1",
			servers: []nvimServer{nvim("w1:p1", "/src/api"), nvim("w2:p1", "/src/differ")},
			want:    []string{"w1:p1"},
		},
		{
			// A pane moved to another workspace gets a new id, and the nvim
			// in it keeps the old one in its environment. Opening there
			// would report success somewhere the user cannot see.
			name:    "a pane id herdr no longer lists is skipped",
			req:     inWorktree,
			servers: []nvimServer{nvim("w3:p9", "/wt/brave")},
		},
		{
			name:    "differ's own pane is never a destination",
			req:     inWorktree,
			servers: []nvimServer{nvim("w3:p1", "/wt/brave")},
		},
		{
			name:    "an nvim outside herdr says no pane and is skipped",
			req:     inWorktree,
			servers: []nvimServer{{Socket: "/s/x", CWD: "/wt/brave"}},
		},
		{
			name:    "a workspace with no repository still finds its own nvim",
			req:     inNotes,
			servers: []nvimServer{nvim("w10:p1", "/notes/sub"), nvim("w4:p2", "/notes")},
			want:    []string{"w4:p2"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, cand := range herdrCandidates(panes, workspaces, c.servers, c.req, c.target) {
				got = append(got, cand.Pane.PaneID)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("candidates = %q, want %q", got, c.want)
			}
		})
	}
}

// fakeHerdrSocket serves one connection: it records the request line and
// answers with reply, or never answers when reply is empty.
func fakeHerdrSocket(t *testing.T, reply string) (string, <-chan string) {
	t.Helper()
	sock := filepath.Join(shortDir(t), "h.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		line, _ := bufio.NewReader(conn).ReadString('\n')
		got <- line
		if reply == "" {
			time.Sleep(time.Second)
			return
		}
		_, _ = conn.Write([]byte(reply + "\n"))
	}()
	return sock, got
}

// pane.focus is on herdr's socket and nowhere else: `herdr pane focus` only
// moves to a neighbour.
func TestHerdrCall_SendsOneRequestLine(t *testing.T) {
	t.Parallel()
	sock, got := fakeHerdrSocket(t, `{"id":"differ","result":{"type":"ok"}}`)

	if err := herdrCall(context.Background(), 0, sock, "pane.focus", map[string]string{"pane_id": "w2:p1"}); err != nil {
		t.Fatalf("herdrCall: %v", err)
	}
	var req struct {
		ID     string            `json:"id"`
		Method string            `json:"method"`
		Params map[string]string `json:"params"`
	}
	if err := json.Unmarshal([]byte(<-got), &req); err != nil {
		t.Fatalf("request is not JSON: %v", err)
	}
	if req.ID == "" || req.Method != "pane.focus" || req.Params["pane_id"] != "w2:p1" {
		t.Errorf("request = %+v, want pane.focus of w2:p1 with an id", req)
	}
}

func TestHerdrCall_TheErrorIsHerdrsOwn(t *testing.T) {
	t.Parallel()
	sock, _ := fakeHerdrSocket(t, `{"id":"differ","error":{"code":"pane_not_found","message":"pane w9:p9 not found"}}`)

	err := herdrCall(context.Background(), 0, sock, "pane.focus", map[string]string{"pane_id": "w9:p9"})
	if err == nil || !strings.Contains(err.Error(), "pane w9:p9 not found") {
		t.Errorf("err = %v, want herdr's message", err)
	}
}

func TestHerdrCall_ASilentServerTimesOut(t *testing.T) {
	t.Parallel()
	sock, _ := fakeHerdrSocket(t, "")

	start := time.Now()
	err := herdrCall(context.Background(), 100*time.Millisecond, sock, "pane.focus", map[string]string{"pane_id": "w1:p1"})
	if err == nil {
		t.Fatal("want an error from a server that never answers")
	}
	if waited := time.Since(start); waited > 900*time.Millisecond {
		t.Errorf("waited %v, want it bounded by the timeout", waited)
	}
}

func TestHerdrCall_NoSocketIsAnError(t *testing.T) {
	t.Parallel()
	err := herdrCall(context.Background(), 0, "", "pane.focus", map[string]string{"pane_id": "w1:p1"})
	if err == nil || !strings.Contains(err.Error(), "HERDR_SOCKET_PATH") {
		t.Errorf("err = %v, want it to name HERDR_SOCKET_PATH", err)
	}
}

// fakeHerdr is a herdr CLI that answers the two listings from the fixtures,
// says fg is every pane's shell and only process, splits off w3:p7 from a
// pane 140x45 wide, and logs each call to the returned file, one line per call with every argument
// in brackets.
func fakeHerdr(t *testing.T, fg string) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "calls")
	fixtures, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	bin = writeScript(t, dir, "herdr", `{ printf '[%s]' "$@"; echo; } >> '`+log+`'
case "$1 $2" in
"pane split") echo '{"id":"cli:pane:split","result":{"type":"pane_info","pane":{"pane_id":"w3:p7","tab_id":"w3:t1","workspace_id":"w3"}}}' ;;
"pane layout") echo '{"id":"cli:pane:layout","result":{"layout":{"panes":[{"pane_id":"w3:p1","rect":{"height":45,"width":140,"x":0,"y":0}}]},"type":"pane_layout"}}' ;;
"pane run") echo '{"id":"cli:pane:run","result":{"type":"ok"}}' ;;
"pane list") cat '`+fixtures+`/herdr_pane_list.json' ;;
"workspace list") cat '`+fixtures+`/herdr_workspace_list.json' ;;
"pane process-info") echo '{"id":"cli:pane:process_info","result":{"process_info":{"foreground_processes":[{"argv0":"`+fg+`","name":"`+fg+`","pid":1}],"pane_id":"'"$4"'","shell_pid":1},"type":"pane_process_info"}}' ;;
*) echo '{"error":{"code":"unexpected","message":"unexpected call"}}' >&2; exit 1 ;;
esac`)
	return bin, log
}

// herdrReuseSetup is differ in w3:p1 with an nvim in w3:p2 of the same
// workspace, a file to open, a fake herdr whose panes run fg, and a fake
// socket that takes pane.focus.
func herdrReuseSetup(t *testing.T, fg string) (req Request, nvimSock string, focus <-chan string) {
	t.Helper()
	runtime := shortDir(t)
	nvimSock = filepath.Join(runtime, "nvim.me", "x", "nvim.1.0")
	nvimListeningOn(t, nvimSock, []string{"HERDR_PANE_ID=w3:p2"})

	repo := shortDir(t)
	if err := os.WriteFile(filepath.Join(repo, "src.go"), []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin, _ := fakeHerdr(t, fg)
	apiSock, focus := fakeHerdrSocket(t, `{"id":"differ","result":{"type":"ok"}}`)
	return Request{File: "src.go", Repo: repo, Line: 3, Env: Env{
		Editor: "nvim", XDGRuntimeDir: runtime, User: "me",
		InHerdr: true, HerdrPane: "w3:p1", HerdrWorkspace: "w3", HerdrBin: bin, HerdrSocket: apiSock,
	}}, nvimSock, focus
}

func TestResolve_HerdrReusesTheNvimInTheWorkspace(t *testing.T) {
	t.Parallel()
	req, nvimSock, focus := herdrReuseSetup(t, "nvim")

	plan, err := Resolve(context.Background(), Config{}, req)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if plan.Strategy != StrategyReuse {
		t.Fatalf("Strategy = %q, want reuse", plan.Strategy)
	}
	if !strings.Contains(plan.Desc, "wt-brave") {
		t.Errorf("Desc = %q, want it to name the workspace", plan.Desc)
	}
	if err := plan.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	where, err := queryNvim(context.Background(), 0, nvimSock, `expand("%:t") . ":" . line(".")`)
	if err != nil {
		t.Fatal(err)
	}
	if where != "src.go:3" {
		t.Errorf("nvim is at %q, want src.go:3", where)
	}
	if req := <-focus; !strings.Contains(req, `"pane.focus"`) || !strings.Contains(req, `"w3:p2"`) {
		t.Errorf("focus request = %s, want pane.focus of w3:p2", req)
	}
}

// An nvim suspended with ctrl-z still answers on its socket, but the pane
// shows a shell: the file would open somewhere the user cannot see. herdr
// says what is in the foreground, which is tmux's pane_current_command check.
func TestResolve_HerdrSkipsAnNvimThatIsNotInTheForeground(t *testing.T) {
	t.Parallel()
	req, _, _ := herdrReuseSetup(t, "zsh")

	_, err := Resolve(context.Background(), Config{Strategy: "reuse"}, req)
	if err == nil || !strings.Contains(err.Error(), "set editor_strategy to window") {
		t.Errorf("err = %v, want an actionable error", err)
	}
}

// herdr's window is a new pane beside differ's, in differ's own tab: it never
// takes the user out of the workspace they are in. The editor is exec'd so
// the pane closes when it quits, and quoted for the shell the pane runs —
// fish does not read a POSIX '\” escape.
func TestResolve_HerdrWindowSplitsDiffersPane(t *testing.T) {
	t.Parallel()
	cases := []struct {
		shell string
		quote func([]string) string
	}{
		{"zsh", shellQuote},
		{"fish", fishQuote},
	}
	for _, c := range cases {
		t.Run(c.shell, func(t *testing.T) {
			t.Parallel()
			repo := repoWith(t, "it's a file.go")
			bin, log := fakeHerdr(t, c.shell)
			ed := stubEditor(t, "nvim")
			req := Request{File: "it's a file.go", Repo: repo, Line: 3, Env: Env{
				Editor: ed, InHerdr: true, HerdrPane: "w3:p1", HerdrWorkspace: "w3", HerdrBin: bin,
			}}

			plan, err := Resolve(context.Background(), Config{Strategy: "window"}, req)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if err := runPlan(t, context.Background(), plan); err != nil {
				t.Fatalf("Run: %v", err)
			}

			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			argv := []string{ed, "+3", filepath.Join(repo, "it's a file.go")}
			want := "[pane][layout][--pane][w3:p1]\n" +
				"[pane][split][--pane][w3:p1][--direction][right][--cwd][" + repo + "][--focus]\n" +
				"[pane][process-info][--pane][w3:p7]\n" +
				"[pane][run][w3:p7][exec " + c.quote(argv) + "]\n"
			if string(calls) != want {
				t.Errorf("herdr calls:\n%s\nwant:\n%s", calls, want)
			}
		})
	}
}

func TestSplitDirection(t *testing.T) {
	t.Parallel()
	// Cells are about twice as tall as wide: 140x45 is wide, 80x45 is not.
	for _, c := range []struct {
		w, h int
		want string
	}{{140, 45, "right"}, {80, 45, "down"}, {90, 45, "right"}, {0, 0, "right"}} {
		if got := splitDirection(c.w, c.h); got != c.want {
			t.Errorf("splitDirection(%d, %d) = %q, want %q", c.w, c.h, got, c.want)
		}
	}
}

func TestFishQuote(t *testing.T) {
	t.Parallel()
	args := []string{"plain", "with space", "it's", `back\slash`, `$HOME "q" ; | *`}
	want := `'plain' 'with space' 'it\'s' 'back\\slash' '$HOME "q" ; | *'`
	if got := fishQuote(args); got != want {
		t.Errorf("fishQuote = %s, want %s", got, want)
	}
	if _, err := exec.LookPath("fish"); err != nil {
		return
	}
	out, err := exec.Command("fish", "-c", `printf '%s\n' `+fishQuote(args)).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"); !reflect.DeepEqual(got, args) {
		t.Errorf("fish read %q, want %q", got, args)
	}
}

func TestShellQuote_SurvivesASh(t *testing.T) {
	t.Parallel()
	args := []string{"plain", "with space", "it's", `$HOME "q" \ ; | *`}
	out, err := exec.Command("sh", "-c", `printf '%s\n' `+shellQuote(args)).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"); !reflect.DeepEqual(got, args) {
		t.Errorf("sh read %q, want %q", got, args)
	}
}

// An editor_cmd that is itself a herdr command is a mechanism, exactly as a
// tmux one is, and runs as written.
func TestResolve_AHerdrEditorCmdRunsVerbatim(t *testing.T) {
	t.Parallel()
	root := repoWith(t, "src.ts")
	bin := writeScript(t, t.TempDir(), "herdr", "exit 0")

	plan, err := Resolve(context.Background(), Config{
		Cmd: bin + " pane run w1:p1 {file}",
	}, Request{File: "src.ts", Repo: root, Env: Env{InHerdr: true, HerdrPane: "w3:p1"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if plan.Strategy != StrategyCustom {
		t.Errorf("Strategy = %q, want %q", plan.Strategy, StrategyCustom)
	}
}
