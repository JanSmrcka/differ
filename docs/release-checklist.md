# Release checklist

What to verify before tagging, and how — so the next person does not have to
work out which parts a test suite already covers and which it cannot.

Run everything from a clean checkout.

## Automated

These are in the suite or one command away. A release with any of them failing
is not a release.

| Check | Command |
| --- | --- |
| Tests | `make test` |
| Vet | `go vet ./...` |
| Formatting | `gofmt -l .` (must print nothing) |
| Lint | `GOTOOLCHAIN=go1.25.5 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.10.1 run ./...` |
| Race | `go test -race ./internal/ui/ ./internal/git/` |
| Build | `make build VERSION=$(git describe --tags --always)` |
| Version injection | `./bin/differ --version` names that version |
| Clean-checkout build | `rm -rf /tmp/x && git clone . /tmp/x && cd /tmp/x && go build .` |
| `go install`, local | `GOBIN=/tmp/b go install -ldflags "-X github.com/jansmrcka/differ/cmd.version=vX" .` |

### `go install …@latest`

The form the README tells people to run resolves through the module proxy, so
it only exercises a **tag**, never the working tree:

```bash
GOBIN=/tmp/b go install github.com/jansmrcka/differ@latest && /tmp/b/differ --version
```

It has to report the tag. Up to and including v1.6.0 it reported `dev`:
`rootCmd`'s `Version` field was set in its composite literal, which is
evaluated when the package variable is initialised — before the `init()` that
assigns the `debug.ReadBuildInfo()` fallback. Builds through the Makefile were
unaffected, because ldflags set the variable before either ran, so every local
check passed. Fixed, and `resolveVersion` is now a tested pure function — but
the proxy path itself can only be confirmed against a tag, so **check this
first after tagging and treat a `dev` here as a blocker.**

Two documentation checks are also tests rather than habits, because both had
already drifted once:

- `keymap_test.go` fails if a key is handled but undocumented, documented but
  unhandled, bound twice in one mode, or missing from the README table.
- `config/readme_test.go` fails if a config key is missing from the README's
  sample block, or the sample sets a key that does not exist.

## After tagging

The tag drives `.github/workflows/release.yml` → goreleaser → four archives, a
checksums file, and a generated Homebrew formula in `jansmrcka/homebrew-tap`.

```bash
gh release view vX.Y.Z --json assets -q '.assets[].name'   # 4 archives + checksums.txt
gh release download vX.Y.Z --pattern 'differ_darwin_arm64.tar.gz' --pattern checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
tar xzf differ_darwin_arm64.tar.gz && ./differ --version    # names X.Y.Z, no leading v
```

Then run that unpacked binary against a repository outside the source tree —
it is the artifact people actually get, and it must not depend on anything in
the checkout.

```bash
brew install jansmrcka/tap/differ && differ --version    # then: brew uninstall differ
```

Worth doing on the *previous* release too, before tagging: the tap's current
formula is installable today, so the install path can be proven working
independently of whether this release's formula exists yet. What needs the new
tag is only that the formula has been regenerated at the new version.

**Not covered by any of the above:** a clean install on a machine that has
never had the repository. Running the unpacked artifact from outside the source
tree is close but not the same thing — it shares a Go toolchain, a `$HOME` and
a `$PATH` with the checkout. If a second machine is available, this is the item
to spend it on.

Nothing here checks that the README's install instructions match what works.
That is the gap `go install …@latest` fell through: the checklist had a
*variant* of the command that passed while the documented one did not.

## Manual, because nothing else can check them

A unit test cannot press a key or read a clipboard. Each of these is a real
external integration.

- **tmux feedback.** `feedback_target: tmux`, comment on a line, `S`, and the
  text arrives in the agent's pane. `H` says where it went and whether it
  landed. This *is* reachable from a test that drives a real tmux session —
  see below — but the version that matters is a real agent in a real pane.
- **tmux `display-popup`.** Measured rather than guessed: inside a popup
  `$TMUX_PANE` is **empty** while `$TMUX` is still set. So `selfPane` is empty
  and the "refuse to send to differ's own pane" guard is not confused but
  *silently off*. Whether delivery still lands where the user expects, and what
  the editor's reuse strategy does there, is untested and unverified.
- **Clipboard.** `feedback_target: clipboard` on macOS (`pbcopy`) and on Linux
  (`wl-copy`/`xclip`/`xsel`), including over SSH where there may be no
  clipboard at all — the failure has to be a message, not a lost comment.
- **The editor.** `e` on a file, with and without tmux, with `editor_strategy`
  at each of `auto`, `reuse`, `window` and `inline`.
- **Every git flow, by hand:** stage, unstage, stage all, commit, branch
  switch, branch create, push, pull, the log browser, `-r` comparison, and
  untracked files. The `internal/git` suite covers each operation — `Push` and
  `Pull` only since this checklist was written; the claim was made before it
  was true — and what it cannot cover is the sequence of keystrokes that
  reaches them.
- **Both theme paths.** `--theme gruvbox` and `"theme": "gruvbox"` in the
  config, and that an unknown name is refused with the list — including under
  `--no-color`, where the validation used to be skipped.
- **`--help` for every subcommand**: `differ`, `differ review`, `differ log`,
  `differ commit`.

### What the suite already covers here

Two of the manual items are less manual than they look, and running these first
narrows what the by-hand pass has to establish:

- `TestTmuxTarget_DeliversPayloadToThePane` starts a real detached tmux session
  whose pane appends to a file, sends through the real target, and asserts on
  what landed. It skips when tmux is missing, so it does not run in CI —
  `ubuntu-latest` has no tmux — which means it only ever runs on someone's
  machine. Run it deliberately.
- `internal/feedback/target_test.go` resolves the clipboard target and skips
  when the machine has no clipboard command.

So the manual pass is really about the things neither can reach: a real agent
in a real pane, a popup, SSH with no clipboard, and the keystroke sequences
that get you to them.
