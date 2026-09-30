package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The three forms in the README's table have to be the three Reference
// produces.
//
// The table was added by the commit that introduced the distinction and
// checked by nothing: round 1's exact bug could be reintroduced there — a
// reference with no space — with the suite green, and so could a file-only
// row given a line number.
func TestREADME_TheReferenceTableMatchesReference(t *testing.T) {
	t.Parallel()
	readme := readReadme(t)
	base := Comment{File: "src/cache.ts", StartLine: 12, EndLine: 12}

	for _, tc := range []struct {
		row    string
		locate Locate
	}{
		{"the line resolves", LocateLine},
		{"only the file does", LocateFile},
		{"nothing does", LocateNone},
	} {
		line := rowContaining(t, readme, tc.row)
		c := base
		c.Locate = tc.locate
		want := Reference(c)

		if want == "" {
			if strings.Contains(line, "@") {
				t.Errorf("the %q row shows a reference, and there is none: %q", tc.row, line)
			}
			continue
		}
		if !strings.Contains(line, "`"+want+"`") {
			t.Errorf("the %q row does not show %q: %q", tc.row, want, line)
		}
	}
}

// And the claim about the other integration has to stay true: claudecode.nvim
// sends at_mentioned as JSON-RPC over a websocket and has no text form at all.
//
// Checked as a claim rather than as a blacklist of two phrasings. The
// blacklist version let any rewording of the false claim straight through,
// which is the shape of the original defect; a first attempt at this version
// still let one through, because it accepted any clause that went on to
// mention JSON-RPC and the real sentence does exactly that after an em dash.
// So the text is cut at em dashes and semicolons too, and every clause that
// names the integration and the @ form has to carry a negation.
func TestREADME_DoesNotClaimTheWrongIntegration(t *testing.T) {
	t.Parallel()
	readme := normaliseNames(strings.ToLower(readReadme(t)))

	if !strings.Contains(readme, "does not use this form") {
		t.Fatal("the README no longer says which integration does not use this form")
	}

	seen := 0
	for _, clause := range splitClauses(readme) {
		if !strings.Contains(clause, "claude code") || !mentionsTheTextForm(clause) {
			continue
		}
		seen++
		if !negated(clause) {
			t.Errorf("the README says Claude Code's integration uses this text form: %q",
				strings.TrimSpace(clause))
		}
	}
	// Round 4 found the loop inspecting exactly one clause of the whole
	// README while looking like a general check. If it ever inspects none,
	// the sentinel above is the only thing left and this test means nothing.
	if seen == 0 {
		t.Error("no clause of the README was examined — the check is inspecting nothing")
	}
}

// normaliseNames spells the integration the way the loop looks for it.
//
// "claudecode.nvim" is the project's actual name and does not contain
// "claude code", so the clause naming it was skipped — and round 2's exact
// false claim, written about claudecode.nvim, went straight back in with the
// suite green.
// And the dot in a project's name is not the end of a sentence: splitting on
// it cut "claudecode.nvim" in two, so neither half named both the
// integration and the @ form and the clause was skipped by both tests.
func normaliseNames(text string) string {
	for _, r := range []struct{ from, to string }{
		{"claudecode.nvim", "claude code nvim"},
		{"sidekick.nvim", "sidekick nvim"},
		{"claudecode", "claude code"},
	} {
		text = strings.ReplaceAll(text, r.from, r.to)
	}
	return text
}

// splitClauses cuts prose at the punctuation that separates one assertion
// from the next, em dashes included: "X does not use this form — it sends
// JSON-RPC" is two claims, and reading it as one let a false first half hide
// behind a true second.
func splitClauses(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == ';' || r == '\n' || r == '\u2014'
	})
}

// mentionsTheTextForm reports whether a clause is about the @-mention form
// rather than about Claude Code generally.
func mentionsTheTextForm(clause string) bool {
	for _, marker := range []string{"this form", "@", "reference", "at_mentioned"} {
		if strings.Contains(clause, marker) {
			return true
		}
	}
	return false
}

// negated reports whether a clause denies rather than asserts.
//
// Not " no ": it matched any incidental "no", so "picks this reference up
// with no trouble" and "no configuration needed" both read as denials. A
// negation of a verb is what this is looking for.
func negated(clause string) bool {
	for _, no := range []string{" not ", "n't ", " never ", " cannot ", " nothing "} {
		if strings.Contains(clause, no) {
			return true
		}
	}
	return false
}

// rowContaining returns the README line holding text, failing if there is not
// exactly one.
func rowContaining(t *testing.T, readme, text string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(readme, "\n") {
		if strings.Contains(line, text) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one README line containing %q, found %d", text, len(found))
	}
	return found[0]
}

func readReadme(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	return string(raw)
}
