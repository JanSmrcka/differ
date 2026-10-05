# differ

Review Git changes in the terminal — especially changes a coding agent made
while you were doing something else.

You read the changes file by file, comment on the lines that need it, and send
those comments to the agent's pane in one keystroke. differ tracks what you
have read and carries the result; it does not review anything for you. No
model reads your diff.

It is also an ordinary diff viewer: syntax highlighting, split view, staging,
committing, branch switching, commit log.

<p align="center">
  <img src="./assets/preview.png" alt="differ preview" width="800" />
</p>

## Install

```bash
brew install jansmrcka/tap/differ
go install github.com/jansmrcka/differ@latest
```

Or from source: `make build` (→ `bin/differ`) or `make install` (→ `$GOPATH/bin`).

## Usage

```bash
differ              # all changes (staged + unstaged + untracked)
differ -s           # staged only
differ -r main      # compare against a branch, tag or commit
differ -c           # open straight into the commit message
differ review       # the same changes, straight into the first diff
differ log          # browse recent commits
differ commit       # review what is staged, then commit
```

`review` takes `-s` and `-r` too. `--no-color`, or `NO_COLOR` set to anything,
turns colour off — nothing in differ is distinguished by colour alone.

Exit codes: **0** fine, **1** a runtime problem (not a repository, no such
ref), **2** a bad command line.

## The review loop

```
differ review        # the agent's changes, opened on the first diff
j / k                # move down the diff
c                    # comment on this line — type, then ctrl+s
J                    # next file; the one you left is marked read
S                    # send every pending comment to the agent
```

What that gives you over `git diff`:

- **It remembers where you got to.** Every file is `unreviewed`, `read`,
  `commented`, `sent` or `changed`, and the bar keeps the count:
  `3/7 reviewed  2 comments  1 pending  1 changed`.
- **Comments attach to lines.** They show inline under the line they are about
  and stay `pending` until you send them.
- **The diff does not move under your comments.** A diff with nothing written
  against it follows the agent live. Once you have commented on a file, an
  agent rewrite keeps your place and the bar says `diff moved (2 more
  added) — R to reload`. Comments follow their line either way; one whose line
  is gone goes `stale` rather than pointing at whatever took its place.
- **They survive `q`.** The review is written to `.git/differ/review.json` on
  every change, so a crash, a closed tmux window or an accidental quit costs
  nothing. A comment comes back only if the code it was written about is
  byte-for-byte unchanged — otherwise the file goes back to unreviewed,
  because it needs reading again.

Nothing here touches git. Staging and committing stay explicit.

## Sending to an agent

`A` lists the coding agents running in tmux and sends the review to the one
you pick.

### Choosing the agent

```
 agent

▌ differ:2  claude  /Users/you/git/private/differ
  ELI-panda:2  claude  /Users/you/git/work/ELI-panda
  personal-web:2 %5  opencode  /Users/you/git/private/personal-web
  personal-web:2 %6  claude  /Users/you/git/private/personal-web

 j/k · enter chooses · esc cancels
```

The choice applies at once and is written to the config, so it survives a
restart. The agent in differ's own tmux session sorts first, then one working
in this repository; two in the same window are told apart by pane id.

Discovery walks each pane's **process tree** rather than reading its current
command, so an agent started through `npx`, a shell function or a wrapper
script is still found. It knows `claude`, `codex`, `gemini`, `copilot`,
`opencode` and `aider`, including forms like `npx @anthropic-ai/claude-code`
and `uvx --from aider-chat aider`. It never starts an agent, and it will not
offer a one-shot such as `claude -p`.

What the agent receives, per comment:

```
@src/session.ts :L8

this drops the error instead of returning it — the caller cannot tell
```

The reference carries the file and the line, so nothing repeats them and the
agent reads the code itself. It is the form `sidekick.nvim` emits, and **the
space before the colon matters**: without it the resolver reads `@path:9` as
one filename and attaches nothing. Claude Code's Neovim integration does not
use this form at all — it sends JSON-RPC over a websocket, which is a
different channel.

How much of a reference a comment gets depends on what still resolves:

| | |
|---|---|
| the line resolves | `@src/cache.ts :L12` |
| only the file does | `@src/cache.ts` |
| nothing does | no reference; the prose still says where it was |

The middle case is a line number that would be wrong: the old side of a diff,
or the index under `-s` when the working tree has moved on. Those comments
carry the hunk instead, since it is then the only thing that says which code
is meant:

```
@src/api/client.ts
Line: 10 (old)

Changed code:
-  return fetch(url)
+  return await fetch(url)

Comment:
this needs awaiting too
```

`s` sends one comment, `S` all of them, `H` shows what was sent and whether it
arrived. A failed send never discards anything — the comments stay pending.
differ pastes into the agent's prompt and does **not** press Enter, so a
misconfigured target cannot execute anything.

## Keyboard shortcuts

`?` lists the current view's keys. The bar along the bottom shows the common
ones and hides what would do nothing; a picker or input shows its own. The
tables below are checked against the code — a test fails if a key here has no
handler, or a handler is missing here.

### Everywhere

Except while typing.

| Key      | Action                                       |
| -------- | -------------------------------------------- |
| `?`      | every key for this view                      |
| `H`      | what has been sent, and whether it got there |
| `!`      | the last failure, in full                    |
| `t`      | theme picker                                 |
| `A`      | choose the agent reviews go to               |
| `ctrl+c` | quit immediately                             |

### File List

```
 ● M login.ts                +12 -4
   M a/index.ts               +8 -2
   A format.ts               +31 -0
   ? NOTES.md                 +3 -0
```

`●` is staged. Once a file has been read, the right-hand column shows review
state instead — `read`, `2 comments`, `sent`, `changed`.

| Key           | Action                                     |
| ------------- | ------------------------------------------ |
| `j/k`         | navigate files                             |
| `enter` / `l` | view diff                                  |
| `tab`         | stage/unstage file                         |
| `a`           | stage all                                  |
| `C`           | commit (AI-generated message via `claude`) |
| `b`           | open branch picker                         |
| `v`           | toggle split (side-by-side) diff           |
| `e`           | open in editor — differ keeps running      |
| `P`           | push (auto `--set-upstream` if needed)     |
| `F`           | pull (fast-forward only)                   |
| `g/G`         | first/last file                            |
| `q`           | quit                                       |

### Diff View

Reading and reviewing are one view: comment on the line you are reading.
Nothing here changes git state except `tab`.

| Key         | Action                              |
| ----------- | ----------------------------------- |
| `j/k`       | move line cursor                    |
| `}` / `{`   | next/prev hunk                      |
| `d/u`       | half page down/up                   |
| `g/G`       | first/last line                     |
| `J/K`       | next/prev file                      |
| `c`         | comment on line (edit existing)     |
| `C`         | comment on whole hunk               |
| `x`         | delete comment under cursor         |
| `s`         | send comment under cursor           |
| `S`         | send all pending comments           |
| `R`         | reload after the file changed       |
| `tab`       | stage/unstage                       |
| `b`         | open branch picker                  |
| `v`         | toggle split diff                   |
| `e`         | open in editor at the cursor's line |
| `esc` / `h` | back to file list                   |
| `q`         | quit                                |

Marks in the gutter: `▌` the line cursor, `●` a comment on this line, `!` a
comment that no longer matches the diff. At the end of the code: `›` the line
was cut to fit, `·` trailing whitespace.

Split view shades the part of a line that actually differs, so a
one-character change does not read as a rewrite. It falls back to unified when
the panel is too narrow for two columns.

### Commit Mode

| Key     | Action         |
| ------- | -------------- |
| `enter` | confirm commit |
| `esc`   | cancel         |

### Branch Picker

```
╭───────────────────────────────────────────────────────╮
│  branch                                               │
│                                                       │
│  > feat/                                        12/34 │
│                                                       │
│ ▌*  feat/roadmap-59                                   │
│     feat/payload-90                                   │
│     feat/agent-picker-84                              │
│                                                       │
│  type filters · ↑/↓ · enter switches · ^n new · esc   │
╰───────────────────────────────────────────────────────╯
```

| Key             | Action                                                       |
| --------------- | ------------------------------------------------------------ |
| type            | filter branches                                              |
| `up/down`       | navigate                                                     |
| `ctrl+k/ctrl+j` | navigate                                                     |
| `enter`         | switch branch (asks again if uncommitted changes would move) |
| `ctrl+n`        | create new branch                                            |
| `esc`           | clear filter / close                                         |

### Log

`differ log` browses recent commits; `enter` shows one through the same diff
renderer.

| Key     | Action                        |
| ------- | ----------------------------- |
| `j/k`   | move through commits          |
| `g/G`   | newest/oldest                 |
| `enter` | view the commit's diff        |
| `esc`   | back to the list, from a diff |
| `q`     | quit                          |

## Themes

```bash
differ --theme gruvbox
```

`mocha` (default, also `dark`), `latte` (also `light`), `gruvbox`,
`tokyonight`, `github`. Each is paired with the matching Chroma style so the
chrome and the syntax highlighting agree, and all five are held to a contrast
floor by a test.

`t` opens a picker that repaints the screen as you move, so you choose by
looking rather than by name. `enter` keeps it and writes it to the config.

## Configuration

Config file: `~/.config/differ/config.json`

```json
{
  "theme": "mocha",
  "tab_width": 4,
  "commit_msg_cmd": "claude -p",
  "commit_msg_prompt": "Write a concise git commit message for this diff:",
  "editor_cmd": "",
  "editor_strategy": "auto",
  "editor_panes": [],
  "editor_target": "session",
  "editor_line_args": "",
  "editor_timeout_ms": 0,
  "editor_probe_timeout_ms": 0,
  "split_diff": false,
  "feedback_target": "clipboard",
  "tmux_target": ""
}
```

`feedback_target` is where a review goes: `clipboard` (default), `stdout` or
`tmux`. `A` sets it for you. The clipboard target shells out to `pbcopy` or
`wl-copy`/`xclip`/`xsel`, so it does the right thing over SSH.

`commit_msg_cmd` generates a commit message from the staged diff when you
press `C`; it is pre-filled for you to edit. Any command that reads a diff on
stdin works, and differ carries on without one.

## Editor

`e` opens the file under the cursor — at the cursor's line, from the diff or a
review. differ keeps running.

Which editor: `editor_cmd`, else `$EDITOR`, else `vi`. `editor_cmd` supports
`{file}`, `{repo}` and `{line}`, substituted inside a single argument so
spaces need no quoting. Without `{file}` differ adds the line itself for
editors it knows (`+<line>` for vi-likes, `--goto` for `code`);
`editor_line_args` is the escape hatch for one it does not:

```json
{ "editor_line_args": "{file}:{line}" }     // helix, sublime
{ "editor_line_args": "+{line} {file}" }    // emacs, micro
```

Where it opens — `editor_strategy`:

| value | what happens |
|---|---|
| `auto` (default) | reuse an nvim already open in this tmux session → else a new tmux window → else take over differ's terminal |
| `reuse` | only reuse; say so when there is nothing to reuse |
| `window` | always a new tmux window |
| `inline` | take over differ's terminal, resume when the editor exits |
| `detach` | run it in the background and carry on |

Outside tmux, `reuse` and `window` are refused with a message rather than
silently downgraded. Reuse needs nvim's RPC socket and sends `:drop`, so
nothing unsaved is ever at risk. `editor_panes` and `editor_target` adjust
which panes it will consider and how far it reaches.

GUI editors reuse their own window already, so give them `detach`:

```json
{ "editor_cmd": "code -r --goto {file}:{line}", "editor_strategy": "detach" }
{ "editor_cmd": "zed {file}:{line}",            "editor_strategy": "detach" }
```

An `editor_cmd` starting with `tmux` runs exactly as written and ignores
`editor_strategy`.

## tmux

The layout differ is built for is an agent in one pane and differ in another.
To open it as a popup:

```tmux
bind g display-popup -E -w 90% -h 90% "cd #{pane_current_path} && differ"
```

## When something fails

One line, in differ's own voice, with what to do:

```
push failed  ·  ! details  ·  the remote has commits you do not — pull with F first
```

`!` shows what git, tmux or the clipboard helper actually said. A failure
costs you nothing else — not your place in the diff, not the comments you have
written.

## Contributing

`internal/git` shells out to git; `internal/review` holds the session and
never imports the UI; `internal/feedback` delivers it; `internal/editor`
decides and performs "open this file"; `internal/ui` is the Bubble Tea models,
the diff parser and the renderer.

`CLAUDE.md` has the rules that are not obvious from the code, including a long
list of things that looked right and were not. Read it before changing
anything.
