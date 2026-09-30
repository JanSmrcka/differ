package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jansmrcka/differ/internal/review"
)

// readmeDiff is the change the README's example comment is about: a read that
// used to throw now returns nil.
//
// It is a real unified diff, parsed by the real parser, so the excerpt in the
// README is one the excerpt builder produced. The first version of that block
// was written by hand and was not a hunk any diff could have made — a context
// line, a removal and two additions with no relation to each other — and the
// test in internal/review was made to agree with it by pasting the same
// string into the fixture. The README then documented a payload the program
// could not emit.
const readmeDiff = `diff --git a/src/session.ts b/src/session.ts
--- a/src/session.ts
+++ b/src/session.ts
@@ -1,6 +1,10 @@
 import { readFile } from "node:fs/promises";
 
 export async function loadSession(path: string) {
-  const raw = await readFile(path, "utf8");
-  return JSON.parse(raw);
+  try {
+    const raw = await readFile(path, "utf8");
+    return JSON.parse(raw);
+  } catch {
+    return null;
+  }
 }
`

// The README shows what a comment looks like when it reaches the agent. It is
// checked against the whole pipeline — parser, excerpt builder, FormatComment
// — rather than against a hand-written Comment, so the block cannot document
// an excerpt the builder would never produce.
func TestREADME_ShowsThePayloadTheWholePipelineProduces(t *testing.T) {
	t.Parallel()
	parsed := ParseDiff(readmeDiff)
	if len(parsed.Hunks) != 1 {
		t.Fatalf("fixture should have one hunk, has %d", len(parsed.Hunks))
	}

	want := review.FormatComment(review.Comment{
		File:      "src/session.ts",
		Side:      review.SideNew,
		Locate:    review.LocateLine,
		StartLine: 8,
		EndLine:   8,
		Excerpt:   excerptFor(parsed, parsed.Hunks[0]),
		Body:      "this drops the error instead of returning it — the caller cannot tell",
	})

	block := readmePayloadBlock(t)
	if strings.TrimSpace(block) != strings.TrimSpace(want) {
		t.Errorf("the README's payload block is not what differ produces.\n"+
			"README:\n%s\n\nactual:\n%s", block, want)
	}
}

// The line the README's comment is on must be the line the prose describes:
// the `return null` the comment complains about, not some other row of the
// hunk. Without this the fixture could drift to any line and the block would
// still be internally consistent.
func TestREADME_TheExampleCommentIsOnTheLineItTalksAbout(t *testing.T) {
	t.Parallel()
	parsed := ParseDiff(readmeDiff)
	for _, dl := range parsed.Lines {
		if dl.NewNum == 8 {
			if !strings.Contains(dl.Content, "return null") {
				t.Errorf("new line 8 is %q, not the return the comment is about", dl.Content)
			}
			return
		}
	}
	t.Error("the fixture has no new-side line 8")
}

func readmePayloadBlock(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	const anchor = "What the agent receives, per comment"
	_, after, ok := strings.Cut(string(raw), anchor)
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
