package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The README shows what a comment looks like when it reaches the agent. A
// wrong example there is the same class of defect as a wrong golden — and
// rewriting the reference in it left the whole suite green.
func TestREADME_ShowsThePayloadThatIsActuallySent(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	readme := string(raw)

	// The reference line in the README has to be one Reference could produce.
	const anchor = "What the agent receives, per comment"
	_, after, ok := strings.Cut(readme, anchor)
	if !ok {
		t.Fatalf("the README no longer says %q", anchor)
	}
	_, block, ok := strings.Cut(after, "```")
	if !ok {
		t.Fatal("no payload block follows")
	}
	body, _, ok := strings.Cut(block, "```")
	if !ok {
		t.Fatal("the payload block is never closed")
	}

	var shown string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "@") {
			shown = strings.TrimSpace(line)
			break
		}
	}
	if shown == "" {
		t.Fatalf("the README's payload block shows no reference:\n%s", body)
	}

	// Rebuild it from the File: and Line: lines the same block shows, and
	// require the two to agree.
	file, line := "", 0
	for _, l := range strings.Split(body, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(l), "File: "); ok {
			file = rest
		}
		if rest, ok := strings.CutPrefix(strings.TrimSpace(l), "Line: "); ok {
			num, _, _ := strings.Cut(rest, " ")
			for _, c := range num {
				if c >= '0' && c <= '9' {
					line = line*10 + int(c-'0')
				}
			}
		}
	}
	if file == "" || line == 0 {
		t.Fatalf("could not read File:/Line: out of the block:\n%s", body)
	}

	want := Reference(Comment{File: file, Side: SideNew, StartLine: line, EndLine: line})
	if shown != want {
		t.Errorf("the README shows %q; Reference produces %q", shown, want)
	}
}
