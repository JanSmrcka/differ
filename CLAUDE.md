# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`differ` — terminal UI git diff viewer built with Go + Bubble Tea. Two-panel layout: file list + syntax-highlighted diff.

## Build & Run

```bash
make build        # → bin/differ
make install      # → $GOPATH/bin/differ
go run .          # all changes (staged + unstaged + untracked)
go run . -s       # staged only
go run . -r main  # compare against ref
go run . -c       # open in commit mode
go run . log      # commit browser
go run . commit   # review staged + commit
```

## Test & Lint

```bash
make test              # go test ./...
golangci-lint run      # CI uses v2.10.1, no custom config
```

If the local Go toolchain is newer than go.mod's, golangci-lint fails with
"export data version ... is greater than maximum supported version". Pin the
toolchain to match CI:

```bash
GOTOOLCHAIN=go1.25.5 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.10.1 run ./...
```

Unit tests exist across all packages. Test manually in a real git repo with staged, unstaged, and untracked files.

## Architecture

```
main.go → cmd/root.go (cobra commands)
             ├── internal/config   — Config struct, load/save ~/.config/differ/config.json
             ├── internal/git      — Repo struct, all git ops via os/exec
             ├── internal/theme    — color hex values only (no lipgloss)
             ├── internal/review   — review session: comments, per-file state, feedback text
             ├── internal/feedback — Target interface + clipboard/stdout/tmux delivery
             ├── internal/editor   — decides and performs "open this file in an editor"
             ├── internal/testutil — temp git repos, diff fixtures, golden files (tests only)
             └── internal/ui
                   ├── model.go    — Model state (5 modes: file list / diff / commit / branch / review)
                   ├── update_dispatch.go — Update dispatcher only
                   ├── mode_*.go   — per-mode key handling
                   ├── keymap.go   — the keymap: one table, read by bar/overlay/tests
                   ├── commandbar.go — the one-line command bar and ? overlay
                   ├── history.go  — the H overlay: what was sent, where, and whether it arrived
                   ├── progress.go — review progress: which files moved under the reviewer
                   ├── log.go      — LogModel (commit log browser)
                   ├── diff.go     — diff parser + single-line rendering
                   ├── hunk.go     — hunk model, line addressing, navigation
                   ├── diffrender.go — DiffRenderer: cached rendering, cursor, inline comments
                   ├── comment*.go — building, editing and rendering review comments
                   ├── highlight.go — Chroma syntax highlighting
                   └── styles.go   — all lipgloss styles, bridges theme → lipgloss
```

### Review architecture

`internal/review` and `internal/feedback` must not import `internal/ui`: the UI
owns the diff parser, and a cycle would follow. A `review.Comment` therefore
carries its own anchor text and a plain-text excerpt, which keeps feedback
generation a pure string transformation.

`internal/editor` must not import `internal/ui` either. It both decides how to
open a file and does it — the one exception is handing the terminal to a child
process, which only the bubbletea program can do, so such a plan comes back as
an argv the UI runs through `tea.ExecProcess`. Its environment arrives as an
injected `editor.Env` rather than being read inside, so the whole decision tree
is a pure function of its inputs and its tests need no `t.Setenv` (which would
bar `t.Parallel`).

The frame is the same on every screen, `differ log` included: a header, a
rule, the content, a rule, one bar. There are no boxes — `renderCard` is gone,
and a test fails if a box-drawing corner reappears anywhere.

`DiffRenderer` is the only rendering path — the diff viewer, untracked files
and the commit log browser all go through it, so tab expansion, syntax
highlighting and the cursor cannot diverge between them. There are no
standalone `Render*` helpers any more.

`DiffRenderer` distinguishes **line indexes** (address `ParsedDiff.Lines`; what
the cursor and comments refer to) from **display rows** (what is printed).
Split view pairs two lines onto one row and inline comments insert rows, so the
two diverge — use `RowFor` to map between them, never assume they are equal.

Two Bubble Tea models: `Model` (main diff viewer with file list/diff/commit/branch-picker modes) and `LogModel` (log browser). Both follow `Init()/Update()/View()`. All async work (git calls, AI commit messages) returned as `tea.Cmd` — never block in `Update`.

Version injected via ldflags at build (`-X .../cmd.version`), falls back to `debug.ReadBuildInfo()` for `go install`.

## Architecture Rules

- **Git via shell**: `os/exec.Command("git", ...)` for all git ops. No go-git. Set `cmd.Dir` to repo root.
- **Styles in one place**: all lipgloss styles in `styles.go`, derived from `theme.Theme`. No inline styles.
- **Theme decoupled**: `internal/theme/` defines color values only. `styles.go` bridges to lipgloss.

## Development Process

- **New features and bug fixes**: always use TDD (red-green-refactor). Use `/tdd` skill.
  1. Write failing test first
  2. Implement minimal code to pass
  3. Refactor while keeping tests green

## Code Style

- No global mutable state. Pass config/theme through structs.
- Return errors up, don't panic. User-friendly messages in `cmd/`.
- Keep functions under ~50 lines.
- Use `internal/` for all packages — nothing is public API.

## Dependencies

Only these external deps (don't add more without strong justification):

```
github.com/charmbracelet/bubbletea    # TUI framework
github.com/charmbracelet/bubbles      # viewport, textinput
github.com/charmbracelet/lipgloss     # styling
github.com/alecthomas/chroma/v2       # syntax highlighting
github.com/spf13/cobra                # CLI
```

## UX Priorities

1. **Fast startup** — instant feel. No changes → print one line and exit.
2. **Readable diffs** — syntax highlighting correct. Added/removed with distinct but non-harsh backgrounds.
3. **Keyboard flow** — vim-style (j/k/g/G/d/u). No mouse needed.
4. **Information density** — file status, staged state, line numbers, diff. No decorative waste.

## Common Tasks

### Adding a new keybinding

1. Add it to the table in `internal/ui/keymap.go` — that is the single source
   of truth for the command bar, the `?` overlay and the README.
2. Add the handler to the mode's `update*Mode` method.
3. Run the tests. `keymap_test.go` parses the handlers out of the source and
   fails if a key is handled but undocumented, documented but unhandled, bound
   twice in one mode, marked `Confirm` without actually asking twice, or
   missing from the README table.

### Adding a new git operation

1. Add method to `Repo` in `internal/git/repo.go`
2. Test the git command manually first
3. Handle errors — git commands fail for many reasons

### Adding a new theme

1. Define color values in `internal/theme/theme.go`, add to `Themes` map
2. `styles.go` picks it up automatically

## Gotchas

- **Chroma + lipgloss**: apply Chroma foreground colors token-by-token, keep diff background from line type. Chroma must not override background.
- **Terminal width**: always respect `tea.WindowSizeMsg`. File list panel fixed ~35 chars (`fileListWidth`), diff gets the rest.
- **Viewport**: call `viewport.SetContent()` on content change, `viewport.GotoTop()` on file switch.
- **Unicode width**: use `lipgloss.Width()` not `len()`. In a test, `strings.Index` gives a *byte* offset — measuring a column means `lipgloss.Width(row[:i])`, because the gutter glyphs are multi-byte.
- **`Repo.run` passes `-c core.quotepath=false`.** Without it git escapes non-ASCII bytes in every path it prints, so `žluťoučký.ts` arrived as `"\305\276lu..."`: the file list showed the escaped form and asking git for that file's diff matched nothing.
- **`ctrl+c` is answered in `dispatch`,** before any mode or overlay. In the commit and new-branch inputs the text field swallowed it — bubbletea does not quit on ctrl+c by itself — so `esc` was the only way out. `esc` cancels an input; `ctrl+c` always quits.
- **An overlay owns the keyboard while it is open,** checked outside the typing guard: an async message (the branch list arriving) can switch the mode underneath one, and inside the guard every key then went into that mode's input.
- **Git diff flags**: always `--no-ext-diff --color=never` for predictable output.
- **Untracked files**: no diff available — read file content directly and turn it into an all-added diff with `ParseNewFile()`, then render it through `DiffRenderer` like any other diff.
- **AI commit messages**: runs configurable `commit_msg_cmd` (default `claude -p`). Diff truncated to 8000 chars. Falls back gracefully if CLI unavailable.
