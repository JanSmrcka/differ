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
             ├── internal/review   — review session: comments, per-file state, feedback text,
             │                        and its persistence to .git/differ/review.json
             ├── internal/feedback — Target + Mux interfaces; clipboard/stdout/tmux/herdr
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
                   ├── themepicker.go — the t overlay: try a theme by looking at it
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

A review survives the process. Pending comments, what has been read and the
delivery history are written to `.git/differ/review.json` — located by `git
rev-parse --git-dir`, so a linked worktree keeps its own — on every change to
a comment rather than on quit, since `q` is the one case that was never the
problem. The history is restored unconditionally, because it is the record
that stops the same review going to the agent twice.

**A comment carries the fingerprint of the content its author read**, taken
when the comment is written and stored with it, along with the *scope* it was
measured in. Measuring at save time was wrong twice over: the key was
whatever the agent had written a moment earlier, so a comment came back
presented as valid against a version nobody had seen; and one scope for the
whole file was wrong in both directions — the working tree dropped comments
about staged content that had not moved under `-s`, the mode's own key made a
review written with `differ` unreadable by `differ -s` and then overwrote it.
The scope follows the entry the diff was read from, not the flag differ was
started with, because git lists a file with both staged and unstaged changes
twice. Deciding what "unchanged" means is still the UI's job — it owns the
fingerprints — so `review.Store` is handed a function rather than reaching
for one.

**One differ owns a repository's review.** Every save writes the whole
session over the file; there is no merge, so two instances destroyed each
other's comments and, worse, brought back a comment the other had already
delivered. `review.TakeLock` claims `.git/differ/review.lock` with `O_EXCL`
and this process's pid; a second differ reviews normally, saves nothing and
says so in the bar. A lock naming a pid that is no longer running is taken
over, because a kill is the case the whole feature exists for and must not be
the one that locks the reviewer out.

**Agents live in a multiplexer, and the picker does not know which.**
`feedback.Mux` is discovery (list agents, own pane, where it looked);
`feedback.Target` is delivery. `DetectMux` reads the environment — `HERDR_ENV`
+ `HERDR_PANE_ID`, `TMUX`, and `TERM_PROGRAM` when both are set — with no
subprocess. The UI gets an `Agent` and asks it for its `FeedbackConfig()` and
whether a config `Matches()` it; branching on the mux name in `internal/ui` is
the thing to avoid. Two delivery rules that look contradictory and are not:
**tmux pastes and never presses Enter**, because tmux cannot tell an agent from
a shell; **herdr submits** with `agent prompt`, because herdr can, and refuses
with `agent_blocked` before sending anything. `pane send-text` is not a
substitute — no bracketed paste, so every newline submits. herdr's errors are
JSON on stderr; `HerdrError.Code` survives to the UI (`agent_not_found`
reopens the picker like tmux's "can't find pane"; `agent_prompt_stalled` counts
as delivered, since retrying would send twice). A herdr choice is stored as the
agent's *session id* plus the pane as fallback, and resolved to a pane through
`agent list` at send time, because herdr does not accept a session id as a
target. A target that can also implement `feedback.Watcher` (only herdr) gets a
`agent wait` started after a successful send; its answer lands on the
`review.Delivery` and the comments as `Agent`, and `Model.Close` cancels it.

`internal/editor` must not import `internal/ui` either. It both decides how to
open a file and does it — the one exception is handing the terminal to a child
process, which only the bubbletea program can do, so such a plan comes back as
an argv the UI runs through `tea.ExecProcess`. Its environment arrives as an
injected `editor.Env` rather than being read inside, so the whole decision tree
is a pure function of its inputs and its tests need no `t.Setenv` (which would
bar `t.Parallel`).

The frame is the same on every screen, `differ log` included: a header, a
rule, the content, a rule, one bar. There are no boxes in it — `renderCard` is
gone, and two tests sweep every mode for box-drawing corners.

A **modal** is the one exception, and not a contradiction of that rule: the
rule is about the frame you look at all day, where a border is decoration that
costs a column on each side. A modal is transient and asks for an answer, and
the border is what says the rest of the screen is not taking input. Both
no-boxes sweeps render the base view, with nothing open. `modal.go` draws it —
`lipgloss.PlaceHorizontal` centres, `RoundedBorder` frames, and `placeModal`
does the vertical placement itself because the box has to keep *out* of the
cursor's way rather than sit in the middle — and composites it over the panels
so the view is still visible around it, which is why the comment editor moved
out of the footer: you are commenting on a line you can still see, and the
editor no longer takes rows from the diff to do it. `boxRows` is the one
function that sizes the box, used both to fit the body and to draw it: two
expressions that disagreed handed `fitOverlay` more rows than the box would
show, and it replaced the overflow with a count — the picker's highlighted
row among them.

The **three modals** — the comment editor, the agent picker and the branch
picker — all go through `modal.go`. The branch picker used to be drawn into
the file list's panel, which meant choosing a branch cost you sight of the
changeset, and its new-branch prompt was a footer bar: three shapes for the
same kind of question. `modeBranchPicker` stays, because key routing needs a
mode and `keymap_test.go`'s checks are keyed on one; only where it is drawn
changed. `boxRows` applies its half-area cap **only when the box has a row to
avoid** — that cap exists so the box can move out of the cursor's way, and a
picker is not judged against a particular line, so capping it cost the branch
picker half its rows: at fourteen it had room for the filter and not one
branch.

**A covered row is composited, not cut.** `overlayRow` keeps what is left and
right of the box, and the cut is made by `dropColumns`, which walks the row's
escape sequences and cuts the visible text by display column. Measuring
`MaxWidth`'s output and trimming it as a byte prefix does not work: `MaxWidth`
re-emits the string with its own escapes and a reset, so nothing was trimmed
on any row with more than one styled run — which is every real row — and what
appeared beside the box was the row's own *beginning* repeated. That reads as
real diff, which is worse than the blank it replaced.

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

1. **Fast startup** — instant feel. No changes opens the TUI on an empty state
   that says so (`internal/ui/problem.go`), rather than printing a line and
   exiting: differ is meant to be left running in a pane, and the poll picks
   up the agent's changes as they arrive. `differ commit` and `differ log` do
   print and exit, because neither has anything to wait for.
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
- **The Chroma style belongs to the renderer, not the package.** It was a `sync.Once` (so the session's first theme was the only one that ever took effect), then a package variable behind an `RWMutex`, and both were wrong in the same way: one palette for every renderer. A `DiffRenderer` is built inside a `tea.Cmd` goroutine and resolves its style *then* renders, non-atomically, so a theme switch between the two painted one theme's backgrounds with another's syntax colours — and two parallel tests fought over the variable. `chromaStyleFor(name)` resolves it once per name into a `sync.Map` and `NewDiffRenderer` keeps the result on the renderer. Nil means no highlighting, which is what `--no-color` asks for.
- **Switching theme needs a re-render, not a recolour.** `DiffRenderer` caches every row as a finished string with its styles baked in, so `applyTheme` clears `lastDiffContent` and reloads the diff.
- **Cut a line before highlighting it**, never after: the highlighted string is full of ANSI escapes and slicing it cuts one in half. `renderCode` is the one place a code line is cut, highlighted and marked, and both unified and split go through it — as they do through `stylesFor`, so the two views cannot drift apart again.
- **Cut with `lipgloss`, not with a rune slice.** `clipCode` and `clipRow` use `MaxWidth`, which understands escapes and is linear. The first version dropped one rune at a time and re-measured the whole prefix: 8.8 s for an 80,000-column line, inside `Update`. Slicing runes off an already-styled row also drops the reset and bleeds colour down the screen — the same trap `fitOverlay` hit.
- **Chroma appends a newline to its input** and coalesces it into the last token, so any token running to end of line carries one: the tail of an open block comment, an unterminated string, a CRLF line ending, a non-breaking space. Written out verbatim it turns one row of output into two — `DisplayRows` then disagrees with `Content`, and every row index below it, which is what `RowFor` and the cursor are addressed by, is off by one. `highlightSpan` strips them, which is the only place token values are written.
- **Split a token's value, never the line before lexing.** `highlightSpan` lexes the whole line once and cuts the token *values* at the emphasis boundaries. Splitting the text first changes how it tokenises, so a span starting mid-string would recolour the rest of the line.
- **Lipgloss emits nothing under `go test`** by default — no TTY, so termenv picks its `Ascii` profile and every style renders as bare text. That is not a reason to leave the painting untested: `lipgloss.SetColorProfile(0)` (TrueColor) makes it emit, and `internal/ui/intraline_test.go` asserts on the escapes. Such a test **must not be `t.Parallel()`** — the profile is global and a dozen tests assert on unescaped strings; Go runs parallel tests only after every sequential top-level test returns, so a sequential test cannot overlap them. Restore the previous profile in a `defer`.
- **An underline breaks a grapheme cluster's width.** Lipgloss re-styles run by run when `Underline` is set, which puts an escape between the runes of a ZWJ emoji or a flag — `lipgloss.Width` then measures it as two glyphs instead of one and the row overshoots its budget. `UnderlineSpaces(false)` does not help. Use a background for emphasis inside a line.
- **Every row ends at the panel width.** `clipRow` is the last guard in all three row renderers, because the column budget cannot be satisfied at every width — the line-number block alone is wider than a 10-column panel. A test sweeps widths 1-120 in both views.
- **Line-number width is per diff, not constant.** `geometry` carries it (sized from the largest number the diff mentions, never below `lineNumWidth`) along with the room the row has. Row renderers take it as one value so the next piece of layout does not add another int to five signatures.
- **The poll asks before it acts.** `Repo.Probe` is one `git status
  --porcelain=v2 --branch --untracked-files=all -z`, and the tick does nothing
  else unless its fingerprint moved. It used to rebuild everything every two
  seconds — eight git processes, whatever had happened. Two things make the
  probe complete: `--branch` carries `branch.oid`, `branch.head` and
  `branch.ab`, so a commit, a checkout and a fetch move it without anyone
  asking about the upstream separately; and each named path is `lstat`ed,
  because status reports a modified file's *index* and *HEAD* object ids and
  never hashes the working tree — so editing an already-modified file produces
  byte-identical output. `--untracked-files=all` is not optional either: the
  default collapses an untracked directory to one entry, and an edit inside it
  would move neither the output nor the directory's mtime. `-r <ref>` costs a
  second process, because status describes the worktree against the index and
  HEAD and says nothing about any other ref — without it the fingerprint never
  moved when the ref did and the screen froze for the session. A submodule is
  asked for its own HEAD, because the gitlink oids status reports are the
  superproject's *recorded* commit and stay put however many commits land
  inside.
- **`--no-optional-locks`, or the probe fights the user for the index.** `git
  status` opportunistically rewrites `.git/index` to refresh its stat cache,
  which takes `index.lock` — measured at 8 failed `git add`s in 120 while
  probing in a loop. None of the eight commands the probe replaced wrote the
  index, so that contention would have been new, and it lands on exactly
  differ's user: an agent running git in the same repository.
- **Seeing a change and acting on it are separately paced.** The probe runs
  every tick; the rebuild runs at most every `refreshEvery` ticks. Without the
  second limit a sustained burst moved the fingerprint on every probe and cost
  nine git processes a second — more churn than the two-second rebuild it
  replaced. A fingerprint is only stored once the refresh actually happens, so
  a change held back is delayed, never dropped.
- **Not a filesystem watcher.** Process startup is most of what a git call
  costs — over 200 iterations, `git --version` is 8.5 ms against the probe's
  13.2, and a bare `true` is 1.5 — so the fix for a polling loop is fewer
  processes, not cheaper ones. That is *not* the argument against a watcher,
  though: a watcher's point is the idle ticks, where it would cost nothing
  against one process a second. It is rejected for a dependency CLAUDE.md
  restricts, an fd per directory under kqueue on macOS (fsnotify does not watch
  recursively), and the install walk the issue asks us to avoid.
- **Nothing in `View` may ask git anything.** `renderHeader` called
  `BranchName()`, a synchronous `rev-parse`, so every frame started a process
  and blocked ~8 ms on it — about one per keypress, and the reason #45's "idle
  sessions issue approximately no git subprocesses" did not hold however cheap
  the probe got. The branch is read once at construction and refreshed by the
  handlers that change it. `View` is a pure function of the model; a test takes
  the repo away and renders.
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
