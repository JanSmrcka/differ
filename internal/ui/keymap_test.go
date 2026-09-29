package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jansmrcka/differ/internal/git"
)

// The keymap used to be a hand-written list of hints kept next to, but
// separate from, the switch statements that actually handle keys — and next to
// a third copy in the README. Any of the three could drift.
//
// These tests read the handlers out of the source and compare them with the
// table, so a key that is handled but undocumented, or documented but
// unhandled, fails the build.

// handledKeys parses the UI package and returns, per update function, every
// string a `case` in its key switch matches.
func handledKeys(t *testing.T) map[string][]string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	out := map[string][]string{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !isKeyHandler(fn) {
				continue
			}
			out[fn.Name.Name] = append(out[fn.Name.Name], caseStrings(fn)...)
		}
	}
	return out
}

// isKeyHandler spots a method that takes a tea.KeyMsg, which is how every key
// handler in this package is shaped.
func isKeyHandler(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || fn.Type.Params == nil {
		return false
	}
	for _, p := range fn.Type.Params.List {
		sel, ok := p.Type.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == "KeyMsg" {
			return true
		}
	}
	return false
}

// caseStrings collects the keys a handler actually matches: the cases of a
// `switch msg.String()`, and the literals in a `msg.String() == "P"`
// comparison. Other string switches in the same function are left alone, or
// unrelated literals would look like bindings.
func caseStrings(fn *ast.FuncDecl) []string {
	var keys []string
	ast.Inspect(fn, func(n ast.Node) bool {
		switch c := n.(type) {
		case *ast.SwitchStmt:
			if !isKeyString(c.Tag) {
				return true
			}
			for _, stmt := range c.Body.List {
				clause, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				keys = append(keys, literals(clause.List)...)
			}
		case *ast.BinaryExpr:
			if c.Op != token.EQL {
				return true
			}
			if isKeyString(c.X) || isKeyString(c.Y) {
				keys = append(keys, literals([]ast.Expr{c.X, c.Y})...)
			}
		}
		return true
	})
	return keys
}

// isKeyString reports whether an expression is a call to .String() on the key
// message.
func isKeyString(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "String"
}

func literals(exprs []ast.Expr) []string {
	var out []string
	for _, e := range exprs {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		if s, err := strconv.Unquote(lit.Value); err == nil && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// handlerFor names the function that owns a mode's keys.
var handlerFor = map[viewMode]string{
	modeFileList:     "updateFileListMode",
	modeDiff:         "updateDiffMode",
	modeReview:       "updateReviewMode",
	modeCommit:       "updateCommitMode",
	modeBranchPicker: "updateBranchMode",
}

// Keys handled centrally in routeKey or dispatch rather than per mode, so a
// mode's handler is not expected to carry them.
var globalKeys = map[string]bool{"ctrl+c": true, "?": true, "!": true}

// typingModes are the modes where every printable character is text. ? and !
// are commands everywhere else, and routeKey answers them — but a handler
// claiming one *here* would be an undocumented binding on a key the user
// meant to type, which widening globalKeys stopped catching.
var typingModes = map[viewMode]bool{modeCommit: true, modeBranchPicker: true}

func TestKeymap_NoTypingModeStealsAGlobalKey(t *testing.T) {
	t.Parallel()
	handled := handledKeys(t)

	for mode, fn := range handlerFor {
		if !typingModes[mode] {
			continue
		}
		for _, k := range handled[fn] {
			if k == "?" || k == "!" {
				t.Errorf("%s handles %q, which is a character there, not a command", fn, k)
			}
		}
	}
}

func TestKeymap_EveryHandledKeyIsDocumented(t *testing.T) {
	t.Parallel()
	handled := handledKeys(t)

	for mode, fn := range handlerFor {
		documented := map[string]bool{}
		for _, b := range keymapFor(mode) {
			for _, k := range b.Keys {
				documented[k] = true
			}
		}

		var missing []string
		for _, k := range handled[fn] {
			if globalKeys[k] || documented[k] {
				continue
			}
			missing = append(missing, k)
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s handles keys the keymap does not document: %v", fn, uniq(missing))
		}
	}
}

func TestKeymap_EveryDocumentedKeyIsHandled(t *testing.T) {
	t.Parallel()
	handled := handledKeys(t)

	for mode, fn := range handlerFor {
		// Review falls through to the diff navigation, and the diff to its
		// own; a mode's keys may be handled by anything it delegates to.
		reachable := map[string]bool{}
		for _, f := range append([]string{fn}, delegatesOf(fn)...) {
			for _, k := range handled[f] {
				reachable[k] = true
			}
		}

		var missing []string
		for _, b := range keymapFor(mode) {
			if len(b.Keys) == 0 {
				continue
			}
			if !reachable[b.Keys[0]] {
				missing = append(missing, b.Keys[0])
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("keymap documents keys %v for %s with no handler", missing, fn)
		}
	}
}

// delegatesOf lists the handlers a mode's own handler passes keys on to.
func delegatesOf(fn string) []string {
	switch fn {
	case "updateReviewMode":
		return []string{"diffNavigation", "updateDiffMode"}
	case "updateDiffMode":
		return []string{"diffNavigation"}
	default:
		return nil
	}
}

func uniq(s []string) []string {
	var out []string
	for i, v := range s {
		if i == 0 || s[i-1] != v {
			out = append(out, v)
		}
	}
	return out
}

// A key must not mean two unrelated things in one mode.
func TestKeymap_NoKeyIsBoundTwiceInAMode(t *testing.T) {
	t.Parallel()
	for mode := range handlerFor {
		seen := map[string]string{}
		for _, b := range keymapFor(mode) {
			for _, k := range b.Keys {
				if prev, dup := seen[k]; dup {
					t.Errorf("mode %v binds %q to both %q and %q", mode, k, prev, b.Desc)
				}
				seen[k] = b.Desc
			}
		}
	}
}

// Every mode has to offer a way out, or the user is stuck.
func TestKeymap_EveryModeOffersAnExit(t *testing.T) {
	t.Parallel()
	for mode := range handlerFor {
		found := false
		for _, b := range keymapFor(mode) {
			for _, k := range b.Keys {
				if k == "q" || k == "esc" {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("mode %v documents no q or esc", mode)
		}
	}
}

// Anything the keymap marks as asking again has to actually ask. P did; F
// pulled straight away while the help said otherwise.
func TestKeymap_ConfirmedCommandsReallyAskTwice(t *testing.T) {
	for mode, fn := range handlerFor {
		for _, b := range keymapFor(mode) {
			if !b.Confirm || len(b.Keys) == 0 {
				continue
			}
			k := b.Keys[0]
			m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.go", Status: git.StatusModified}}})
			m.mode = mode
			m.upstream = git.UpstreamInfo{Upstream: "origin/master", Ahead: 1, Behind: 1}

			updated, cmd := m.routeKey(key(k))
			if cmd != nil {
				t.Errorf("%s: %q (%s) acted on the first press", fn, k, b.Desc)
			}
			if got := updated.(Model); got.statusMsg == "" {
				t.Errorf("%s: %q (%s) gave no warning before acting", fn, k, b.Desc)
			}
		}
	}
}

// The README is the third copy of the keymap the audit set out to remove. It
// cannot be generated from here without a build step, so it is verified
// instead: every key the README documents for a view must exist in that
// view's keymap, and every key in the keymap must be in the README.
func TestKeymap_TheREADMEMatchesTheKeymap(t *testing.T) {
	t.Parallel()
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}

	sections := map[viewMode]string{
		modeFileList: "### File List",
		modeDiff:     "### Diff View",
		modeReview:   "### Review Mode",
	}
	for mode, heading := range sections {
		table := sectionTable(t, string(readme), heading)

		documented := map[string]bool{}
		for _, b := range keymapFor(mode) {
			for _, k := range b.Keys {
				documented[k] = true
			}
		}

		for _, k := range table {
			if !documented[k] {
				t.Errorf("%s documents %q, which is not in the keymap", heading, k)
			}
		}

		inREADME := map[string]bool{}
		for _, k := range table {
			inREADME[k] = true
		}
		for _, b := range keymapFor(mode) {
			if len(b.Keys) == 0 {
				continue
			}
			if !inREADME[b.Keys[0]] {
				t.Errorf("%s does not document %q (%s)", heading, b.Keys[0], b.Desc)
			}
		}
	}
}

// sectionTable pulls the backticked keys out of a README table, splitting the
// "`enter` / `l`" and "`j/k`" forms into individual keys.
func sectionTable(t *testing.T, readme, heading string) []string {
	t.Helper()
	i := strings.Index(readme, heading)
	if i < 0 {
		t.Fatalf("README has no %q section", heading)
	}
	rest := readme[i+len(heading):]
	if j := strings.Index(rest, "\n### "); j >= 0 {
		rest = rest[:j]
	}

	// Only the first column of a table headed "Key". Prose in the same section
	// is full of backticks — `claude`, `--set-upstream` — and a section may
	// hold other tables entirely, such as the diff view's two mark legends,
	// whose first column is glyphs rather than keys.
	var keys []string
	inKeyTable := false
	lines := strings.Split(rest, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			inKeyTable = false
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) < 2 {
			continue
		}
		first := strings.TrimSpace(cells[0])
		if isSeparatorRow(line) {
			continue
		}
		// A header row is the one directly above the |---|---| separator, and
		// its first cell decides whether the rows under it are keys. Spotting
		// it by "no backticks in the first cell" looked equivalent and was
		// not: the branch picker's first *data* row is `| type | filter |`,
		// which silently turned that whole table off.
		if i+1 < len(lines) && isSeparatorRow(lines[i+1]) {
			inKeyTable = first == "Key"
			continue
		}
		if !inKeyTable {
			continue
		}
		for _, cell := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(cells[0], -1) {
			for _, k := range strings.Split(cell[1], "/") {
				if k = strings.TrimSpace(k); k != "" {
					keys = append(keys, k)
				}
			}
		}
	}
	return keys
}

// Confirmation flags must not leave each other armed: a destructive action
// firing after a different key was pressed in between is exactly what the
// two-press pattern exists to prevent.
func TestKeymap_ConfirmationsDisarmEachOther(t *testing.T) {
	base := func() Model {
		m := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.go", Status: git.StatusModified}}})
		m.upstream = git.UpstreamInfo{Upstream: "origin/master"}
		return m
	}
	for _, seq := range [][]string{{"P", "F", "P"}, {"F", "P", "F"}, {"P", "j", "P"}, {"F", "j", "F"}} {
		m := base()
		var cmd tea.Cmd
		for _, k := range seq {
			var u tea.Model
			u, cmd = m.updateFileListMode(key(k))
			m = u.(Model)
		}
		if cmd != nil {
			t.Errorf("%v acted on the last press; another key should have disarmed it", seq)
		}
	}
}

// The globals were never checked against the code — the AST tests only look at
// the per-mode tables. ctrl+c is documented as "quit immediately", and in the
// commit input it was swallowed by the text field: esc was the only way out.
func TestKeymap_TheGlobalKeysWorkInEveryMode(t *testing.T) {
	t.Parallel()
	states := []struct {
		name  string
		setup func(m Model) Model
	}{
		{"file list", func(m Model) Model { m.mode = modeFileList; return m }},
		{"diff", func(m Model) Model { m.mode = modeDiff; return m }},
		{"review", func(m Model) Model { m.mode = modeReview; return m }},
		{"commit", func(m Model) Model { m.mode = modeCommit; return m }},
		{"branch picker", func(m Model) Model { m.mode = modeBranchPicker; return m }},
		{"branch create", func(m Model) Model {
			m.mode = modeBranchPicker
			m.branchCreating = true
			return m
		}},
		{"comment editor", func(m Model) Model {
			m.mode = modeReview
			m.commenting = true
			return m
		}},
		{"help overlay", func(m Model) Model { m.showHelp = true; return m }},
		{"history overlay", func(m Model) Model { m.mode = modeReview; m.showHistory = true; return m }},
	}

	for _, s := range states {
		base := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.go", Status: git.StatusModified}}})
		m := s.setup(base)

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if cmd == nil {
			t.Errorf("%s: ctrl+c did nothing", s.name)
			continue
		}
		if _, quit := cmd().(tea.QuitMsg); !quit {
			t.Errorf("%s: ctrl+c did not quit", s.name)
		}
	}

	// ? and ! are global in the same sense the command bar is: everywhere a
	// key is a command rather than a character. Where the user is typing they
	// are text, which is why they are not answered before the mode dispatch
	// the way ctrl+c is.
	for _, s := range states {
		base := newTestModel(t, []fileItem{{change: git.FileChange{Path: "a.go", Status: git.StatusModified}}})
		m := s.setup(base)
		// What is under test is whether the key opens the overlay from this
		// mode, so the states that start with one open are cleared first —
		// there ? would toggle it shut, which is its own test.
		m.showHelp, m.showHistory, m.showProblem = false, false, false
		typing := m.typing()

		for _, c := range []struct {
			key  string
			open func(Model) bool
		}{
			{"?", func(m Model) bool { return m.showHelp }},
			{"!", func(m Model) bool { return m.showProblem }},
		} {
			updated, _ := m.routeKey(key(c.key))
			got := c.open(updated.(Model))
			// An overlay is already open in two of the states, where these
			// keys switch between them rather than being swallowed.
			if want := !typing; got != want {
				t.Errorf("%s: %q opened the overlay = %v, want %v (typing = %v)",
					s.name, c.key, got, want, typing)
			}
		}
	}
}

// isSeparatorRow spots the |---|---| line under a markdown table's header.
func isSeparatorRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	cells := strings.Split(strings.Trim(trimmed, "|"), "|")
	return len(cells) >= 2 && regexp.MustCompile(`^[-: ]+$`).MatchString(strings.TrimSpace(cells[0]))
}

// The README check is only as good as its parser, and a parser that reads a
// section as zero keys passes every assertion in it. This pins what each
// section actually yields.
func TestKeymap_TheREADMEParserReadsEverySection(t *testing.T) {
	t.Parallel()
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []struct {
		heading string
		want    string
	}{
		{"### File List", "tab"},
		{"### Diff View", "}"},
		{"### Review Mode", "C"},
		{"### Commit Mode", "enter"},
		{"### Branch Picker", "ctrl+n"},
	} {
		keys := sectionTable(t, string(readme), section.heading)
		if len(keys) == 0 {
			t.Errorf("%s reads as no keys at all — the parser is blind there", section.heading)
			continue
		}
		found := false
		for _, k := range keys {
			if k == section.want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: parsed %v, which does not include %q", section.heading, keys, section.want)
		}
	}
}
