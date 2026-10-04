package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefault(t *testing.T) {
	t.Parallel()
	cfg := Default()
	if cfg.Theme != "dark" {
		t.Errorf("Theme=%q, want dark", cfg.Theme)
	}
	if cfg.TabWidth != 4 {
		t.Errorf("TabWidth=%d, want 4", cfg.TabWidth)
	}
	if cfg.SplitDiff {
		t.Error("SplitDiff should default to false")
	}
	if cfg.CommitMsgCmd != "" {
		t.Errorf("CommitMsgCmd should be empty, got %q", cfg.CommitMsgCmd)
	}
}

func TestSaveAndLoad(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Config{
		Theme:        "light",
		TabWidth:     8,
		SplitDiff:    true,
		CommitMsgCmd: "echo test",
	}
	if err := SaveTo(cfg, path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	got := LoadFrom(path)
	if got.Theme != "light" {
		t.Errorf("Theme=%q, want light", got.Theme)
	}
	if got.TabWidth != 8 {
		t.Errorf("TabWidth=%d, want 8", got.TabWidth)
	}
	if !got.SplitDiff {
		t.Error("SplitDiff should be true")
	}
	if got.CommitMsgCmd != "echo test" {
		t.Errorf("CommitMsgCmd=%q, want %q", got.CommitMsgCmd, "echo test")
	}
}

func TestLoad_NoFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	cfg := LoadFrom(path)
	if cfg.Theme != "dark" || cfg.TabWidth != 4 {
		t.Error("missing file should return defaults")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadFrom(path)
	if cfg.Theme != "dark" || cfg.TabWidth != 4 {
		t.Error("invalid JSON should return defaults")
	}
}

func TestSave_CreatesDir(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	if err := SaveTo(cfg, path); err != nil {
		t.Fatalf("SaveTo should create dirs: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file should exist: %v", err)
	}
}

// The editor knobs have to survive a save/load round trip, and default to
// zero so an existing config file keeps behaving as it did.
func TestEditorSettings_RoundTripAndDefaults(t *testing.T) {
	d := Default()
	if d.EditorStrategy != "" || d.EditorTarget != "" || d.EditorLineArgs != "" ||
		d.EditorPanes != nil || d.EditorTimeoutMS != 0 || d.EditorProbeTimeoutMS != 0 {
		t.Errorf("Default() should leave the editor settings unset, got %+v", d)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	want := d
	want.EditorStrategy = "reuse"
	want.EditorPanes = []string{"nvim", "hx"}
	want.EditorTarget = "any"
	want.EditorLineArgs = "{file}:{line}"
	want.EditorTimeoutMS = 9000
	want.EditorProbeTimeoutMS = 2500

	if err := SaveTo(want, path); err != nil {
		t.Fatal(err)
	}
	got := LoadFrom(path)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip lost settings:\n got %+v\nwant %+v", got, want)
	}
}

// The herdr choice has keys of its own and survives a round trip.
func TestHerdrSettings_RoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	want := Default()
	want.FeedbackTarget = "herdr"
	want.HerdrTarget = "01729727-58c0-46f8-90ba-5cb2723b2a5a"
	want.HerdrPane = "w2:p2"
	if err := SaveTo(want, path); err != nil {
		t.Fatal(err)
	}
	if got := LoadFrom(path); !reflect.DeepEqual(got, want) {
		t.Errorf("round trip lost settings:\n got %+v\nwant %+v", got, want)
	}
}
