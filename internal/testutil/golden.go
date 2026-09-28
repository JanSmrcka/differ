package testutil

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateGolden regenerates golden files instead of comparing against them:
//
//	go test ./... -update
var updateGolden = flag.Bool("update", false, "rewrite golden files instead of comparing")

// Golden compares got against testdata/<name>.golden, failing the test on any
// difference. With -update it rewrites the file instead.
//
// Intended for deterministic generated text — review feedback payloads above
// all — where an exact-match assertion is more useful than a pile of
// strings.Contains checks.
func Golden(t *testing.T, name, got string) {
	t.Helper()

	if *updateGolden {
		if err := writeGolden(name, got); err != nil {
			t.Fatalf("update golden %s: %v", name, err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath(name))
	if err != nil {
		t.Fatalf("read golden %s: %v\nrun `go test ./... -update` to create it", name, err)
	}
	if err := compareGolden(string(want), got); err != nil {
		t.Errorf("output does not match %s:\n%v", goldenPath(name), err)
	}
}

func goldenPath(name string) string {
	return filepath.Join("testdata", name+".golden")
}

func writeGolden(name, content string) error {
	path := goldenPath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// compareGolden reports the first differing line, which is far easier to read
// than two multi-line blobs.
func compareGolden(want, got string) error {
	if want == got {
		return nil
	}
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		w, g := lineAt(wantLines, i), lineAt(gotLines, i)
		if w == g {
			continue
		}
		return fmt.Errorf("first difference at line %d:\n  want: %q\n  got:  %q", i+1, w, g)
	}
	return fmt.Errorf("content differs but no line differs (want %d lines, got %d)", len(wantLines), len(gotLines))
}

func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return "<missing>"
}
