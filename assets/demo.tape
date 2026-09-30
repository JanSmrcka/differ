# differ — the review loop, for the README.
#
#   brew install vhs
#   ./assets/demo-fixture.sh          # scratch repo + isolated config + tmux panes
#   vhs assets/demo.tape              # writes assets/demo.gif
#
# The fixture is committed so this is reproducible rather than a lucky take:
# the same diff, the same line under the cursor, the same delivery target, and
# an isolated HOME so the recorder's own theme and feedback_target cannot leak
# in. Every keystroke below was verified against a real tmux session by driving
# these panes with send-keys and reading what arrived.

Output assets/demo.gif

Set Shell "bash"
Set FontSize 16
# Wide enough that half of it still clears differ's two-panel threshold of 72
# columns — at a narrower width the layout collapses to one panel and the demo
# would not show the file list beside the diff.
Set Width 1800
Set Height 900
Set Padding 12
Set TypingSpeed 55ms
Set PlaybackSpeed 1.0

# Attach to the layout the fixture built: the agent on the left, a shell on the
# right. Attaching makes the right-hand pane active, which is where differ goes.
Hide
Type "clear && tmux attach -t differ-demo" Enter
Sleep 1500ms
Show

# 1. An agent has been working. What did it do?
Type "differ review"  Sleep 400ms  Enter
Sleep 2500ms

# 2. differ opens straight into review mode on the first changed file.
#    n moves to the next one.
Sleep 800ms
Type "n"
Sleep 1800ms

# 3. Down to the line that is wrong: the catch that swallows the read error
#    and returns null. Nine lines from where the cursor starts — checked
#    against the parser, not counted by eye.
Down Sleep 160ms
Down Sleep 160ms
Down Sleep 160ms
Down Sleep 160ms
Down Sleep 160ms
Down Sleep 160ms
Down Sleep 160ms
Down Sleep 160ms
Down Sleep 1200ms

# 4. Say so.
Type "c"
Sleep 800ms
Type "this drops the error instead of returning it — the caller cannot tell"
Sleep 1200ms
Ctrl+S
Sleep 1500ms

# 5. Send it. It lands in the agent's pane on the left, with the diff context
#    around the line it is about.
Type "S"
Sleep 3000ms

# 6. What was sent, where, and whether it arrived.
Type "H"
Sleep 2500ms
Escape
Sleep 600ms

Type "q"
Sleep 1200ms
