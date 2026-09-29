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
                   ├── problem.go  — how a failure is presented, and the empty states
                   ├── filelist.go — path disambiguation and the file list's own arithmetic
                   ├── intraline.go — what differs *within* a pair of split-view lines
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

1. Define color values in `internal/theme/theme.go`, add it to the `Themes`
   map and to `ThemeNames()`.
2. `styles.go` picks it up automatically.
3. Run the tests. `theme_test.go` walks the whole registry, so the new theme
   is held to the same standard as the others without another test being
   written: every field non-empty and valid hex, a Chroma style that actually
   exists, and the contrast floor on every pair — including the marks drawn
   inside the diff. Take the palette from upstream rather than eyeballing it,
   and record the source and licence next to the constructor.

### Failures

Nothing puts a tool's *whole* output in the status bar, and nothing puts any
of it there unshaped. `Model.fail(action, err)` is the one way a failure
reaches the user: `describe` turns it into a summary,
a hint and the original text, the bar gets the one-line form, and `!` shows the
rest. The hints are matched on fragments of git's wording because git has no
error codes — an unmatched failure still gets presented, so a reworded git
message degrades to one line of its own words — capped, and with git's severity
prefix stripped — rather than to something wrong. That fallback is the one
place a tool's wording reaches the bar, and it is a deliberate last resort.

`fail` touches nothing but the status bar and the stored problem. A failure
must never cost the user their place in the diff or a comment they have
written.

`Repo.run` captures stdout and stderr separately and, on failure, returns
whichever said something — stderr first, then stdout, because `git commit`
writes "no changes added to commit" to stdout. `cmd.Output()` looks like it
does this and does not: it puts stderr on `ExitError.Stderr`, whose `Error()`
renders only "exit status 1". Every hint in the table depends on git's actual
words reaching `describe`, so a test drives real failing git commands all the
way to the status bar rather than handing `describe` an error built by hand.

## Gotchas

- **Chroma + lipgloss**: apply Chroma foreground colors token-by-token, keep diff background from line type. Chroma must not override background.
- **Cut a line before highlighting it**, never after: the highlighted string is full of ANSI escapes and slicing it cuts one in half. `renderCode` is the one place a code line is cut, highlighted and marked, and both unified and split go through it — as they do through `stylesFor`, so the two views cannot drift apart again.
- **Cut with `lipgloss`, not with a rune slice.** `clipCode` and `clipRow` use `MaxWidth`, which understands escapes and is linear. The first version dropped one rune at a time and re-measured the whole prefix: 8.8 s for an 80,000-column line, inside `Update`. Slicing runes off an already-styled row also drops the reset and bleeds colour down the screen — the same trap `fitOverlay` hit.
- **Chroma appends a newline to its input** and coalesces it into the last token, so any token running to end of line carries one: the tail of an open block comment, an unterminated string, a CRLF line ending, a non-breaking space. Written out verbatim it turns one row of output into two — `DisplayRows` then disagrees with `Content`, and every row index below it, which is what `RowFor` and the cursor are addressed by, is off by one. `highlightSpan` strips them, which is the only place token values are written.
- **Split a token's value, never the line before lexing.** `highlightSpan` lexes the whole line once and cuts the token *values* at the emphasis boundaries. Splitting the text first changes how it tokenises, so a span starting mid-string would recolour the rest of the line.
- **Lipgloss emits nothing under `go test`** by default — no TTY, so termenv picks its `Ascii` profile and every style renders as bare text. That is not a reason to leave the painting untested: `lipgloss.SetColorProfile(0)` (TrueColor) makes it emit, and `internal/ui/intraline_test.go` asserts on the escapes. Such a test **must not be `t.Parallel()`** — the profile is global and a dozen tests assert on unescaped strings; Go runs parallel tests only after every sequential top-level test returns, so a sequential test cannot overlap them. Restore the previous profile in a `defer`.
- **An underline breaks a grapheme cluster's width.** Lipgloss re-styles run by run when `Underline` is set, which puts an escape between the runes of a ZWJ emoji or a flag — `lipgloss.Width` then measures it as two glyphs instead of one and the row overshoots its budget. `UnderlineSpaces(false)` does not help. Use a background for emphasis inside a line.
- **Every row ends at the panel width.** `clipRow` is the last guard in all three row renderers, because the column budget cannot be satisfied at every width — the line-number block alone is wider than a 10-column panel. A test sweeps widths 1-120 in both views.
- **Line-number width is per diff, not constant.** `geometry` carries it (sized from the largest number the diff mentions, never below `lineNumWidth`) along with the room the row has. Row renderers take it as one value so the next piece of layout does not add another int to five signatures.
- **Terminal width**: always respect `tea.WindowSizeMsg`. The file list takes `m.listWidth()` — a quarter of the terminal, clamped to `[minListWidth, maxListWidth]` — and the diff gets the rest. It is a method, not a constant: below `twoPanelWidth` the layout collapses to one panel and the width belongs entirely to whichever it is. Ask the model, never assume.
- **Colour is never the only channel.** Every distinction carries a mark or a word as well as a hue: `+`/`-`, the cursor bar, the staged dot, the status letter, the focus bar, and review state as a word. `TestResponsive_EveryDistinctionSurvivesWithoutColour` strips the colour and checks each one is still there.
- **Viewport**: call `viewport.SetContent()` on content change, `viewport.GotoTop()` on file switch.
- **Unicode width**: use `lipgloss.Width()` not `len()`. In a test, `strings.Index` gives a *byte* offset — measuring a column means `lipgloss.Width(row[:i])`, because the gutter glyphs are multi-byte.
- **`Repo.run` passes `-c core.quotepath=false`.** Without it git escapes non-ASCII bytes in every path it prints, so `žluťoučký.ts` arrived as `"\305\276lu..."`: the file list showed the escaped form and asking git for that file's diff matched nothing.
- **`ctrl+c` is answered in `dispatch`,** before any mode or overlay. In the commit and new-branch inputs the text field swallowed it — bubbletea does not quit on ctrl+c by itself — so `esc` was the only way out. `esc` cancels an input; `ctrl+c` always quits.
- **An overlay owns the keyboard while it is open,** checked outside the typing guard: an async message (the branch list arriving) can switch the mode underneath one, and inside the guard every key then went into that mode's input.
- **Git diff flags**: always `--no-ext-diff --color=never` for predictable output.
- **Untracked files**: no diff available — read file content directly and turn it into an all-added diff with `ParseNewFile()`, then render it through `DiffRenderer` like any other diff.
- **AI commit messages**: runs configurable `commit_msg_cmd` (default `claude -p`). Diff truncated to 8000 chars. Falls back gracefully if CLI unavailable.
