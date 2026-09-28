package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGolden_MatchingContentPasses(t *testing.T) {
	// testdata/sample.golden holds exactly this text.
	Golden(t, "sample", "File: src/auth/login.ts\nLines: 43-45\n")
}

func TestCompareGolden(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		got     string
		wantErr bool
	}{
		{name: "identical", want: "a\nb\n", got: "a\nb\n"},
		{name: "differs", want: "a\nb\n", got: "a\nc\n", wantErr: true},
		{name: "trailing newline matters", want: "a\n", got: "a", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := compareGolden(tc.want, tc.got)
			if tc.wantErr && err == nil {
				t.Fatal("expected mismatch error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCompareGolden_ErrorNamesTheFirstDifferingLine(t *testing.T) {
	err := compareGolden("same\nwant-this\n", "same\ngot-that\n")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "line 2") {
		t.Errorf("error should point at line 2, got: %s", msg)
	}
	if !strings.Contains(msg, "want-this") || !strings.Contains(msg, "got-that") {
		t.Errorf("error should show both sides, got: %s", msg)
	}
}

func TestGoldenPath(t *testing.T) {
	if got, want := goldenPath("feedback_single_line"), filepath.Join("testdata", "feedback_single_line.golden"); got != want {
		t.Errorf("goldenPath = %q, want %q", got, want)
	}
}

func TestGolden_UpdateWritesFile(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := writeGolden("brand_new", "hello\n"); err != nil {
		t.Fatalf("writeGolden: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "testdata", "brand_new.golden"))
	if err != nil {
		t.Fatalf("golden not written: %v", err)
	}
	if string(data) != "hello\n" {
		t.Errorf("content = %q", data)
	}
}
