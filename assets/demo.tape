# differ — the review loop, for the README.
#
# Recorded with vhs (https://github.com/charmbracelet/vhs):
#
#   brew install vhs
#   ./assets/demo-fixture.sh          # builds the scratch repo the tape expects
#   vhs assets/demo.tape              # writes assets/demo.gif
#
# The fixture script is committed so the recording is reproducible: the same
# diff, the same cursor positions, the same comment. Re-record it whenever the
# frame changes shape.

Output assets/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 640
Set Padding 12
Set Theme "Catppuccin Mocha"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

# The tape assumes the fixture repo exists and differ is on PATH.
Hide
Type "cd /tmp/differ-demo && clear" Enter
Show

# 1. An agent has been working. What did it do?
Type "differ review"  Sleep 400ms  Enter
Sleep 2s

# 2. Three files changed. Walk into the first one.
Down  Sleep 500ms
Down  Sleep 800ms
Up    Sleep 600ms
Enter Sleep 1.5s

# 3. Down to the line that is wrong.
Down Sleep 200ms
Down Sleep 200ms
Down Sleep 200ms
Down Sleep 200ms
Down Sleep 200ms
Down Sleep 900ms

# 4. Say so.
Type "c" Sleep 700ms
Type "this drops the error instead of returning it — the caller cannot tell"
Sleep 1.2s
Ctrl+S
Sleep 1.5s

# 5. Next file; the one just left is marked read.
Type "n" Sleep 1.5s

# 6. Send everything back to the agent's pane.
Type "S" Sleep 2.5s

# 7. What was sent, and whether it arrived.
Type "H" Sleep 2.5s
Escape Sleep 600ms

Type "q" Sleep 1s
