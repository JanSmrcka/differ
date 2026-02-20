package ui

import "testing"

func TestExtractFilename(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "diff --git a/foo.go b/foo.go", "foo.go"},
		{"nested", "diff --git a/pkg/bar/baz.go b/pkg/bar/baz.go", "pkg/bar/baz.go"},
		{"with_spaces", "diff --git a/some file.go b/some file.go", "some file.go"},
		{"malformed", "not a diff header", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractFilename(tt.input)
			if got != tt.want {
				t.Errorf("extractFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
