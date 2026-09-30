package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The README shows what a comment looks like when it reaches the agent, so it
// is checked against what FormatComment actually produces — the whole block,
// not one line of it.
//
// An earlier version rebuilt only the reference from the File: and Line: lines
// in the same block and compared that. It caught the format bug it was written
// for and nothing else: the block could document an old-side comment carrying
// a line reference, drop the "Changed code:" and "Comment:" sections, put the
// reference in the wrong place, or be swapped wholesale for a made-up example,
// and the test stayed green.
func TestREADME_ShowsThePayloadThatIsActuallySent(t *testing.T) {
	t.Parallel()
	block := readmePayloadBlock(t)

	// The fixture the README documents. Kept here rather than parsed out of
	// the prose: the point is that the README agrees with the code, and
	// deriving the expectation from the README would make it agree with
	// itself.
	want := FormatComment(Comment{
		File:      "src/session.ts",
		Side:      SideNew,
		Locate:    LocateLine,
		StartLine: 9,
		EndLine:   9,
		Excerpt: " export async function loadSession(path: string) {\n" +
			"-  const raw = await readFile(path, \"utf8\");\n" +
			"+  } catch {\n" +
			"+    return null;\n",
		Body: "this drops the error instead of returning it — the caller cannot tell",
	})

	if strings.TrimSpace(block) != strings.TrimSpace(want) {
		t.Errorf("the README's payload block is not what FormatComment produces.\n"+
			"README:\n%s\n\nactual:\n%s", block, want)
	}
}

// And the claim about the other integration has to stay true: claudecode.nvim
// sends at_mentioned as JSON-RPC over a websocket and has no text form at all,
// which an earlier README asserted the opposite of.
func TestREADME_DoesNotClaimTheWrongIntegration(t *testing.T) {
	t.Parallel()
	readme := readReadme(t)
	for _, wrong := range []string{
		"Claude Code's editor integration already use",
		"Claude Code's editor integration uses",
	} {
		if strings.Contains(readme, wrong) {
			t.Errorf("the README claims %q, which is false: that integration "+
				"sends JSON-RPC, not this text form", wrong)
		}
	}
	if !strings.Contains(readme, "does not use this form") {
		t.Error("the README no longer says which integration does not use this form")
	}
}

func readmePayloadBlock(t *testing.T) string {
	t.Helper()
	const anchor = "What the agent receives, per comment"
	_, after, ok := strings.Cut(readReadme(t), anchor)
	if !ok {
		t.Fatalf("the README no longer says %q", anchor)
	}
	_, block, ok := strings.Cut(after, "```\n")
	if !ok {
		t.Fatal("no payload block follows")
	}
	body, _, ok := strings.Cut(block, "```")
	if !ok {
		t.Fatal("the payload block is never closed")
	}
	return body
}

func readReadme(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	return string(raw)
}
