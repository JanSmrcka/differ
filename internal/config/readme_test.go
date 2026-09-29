package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The README is the second copy of the config schema, and the one people read.
//
// Written by hand it drifts: a key added to the struct is undocumented until
// someone remembers, and a key removed lives on in the README as advice that
// does nothing. The keymap has had this test for a while and the config had
// nothing — all fourteen keys happened to be documented when this was written,
// which is exactly when to nail it down.
func TestConfig_TheREADMEDocumentsEveryKey(t *testing.T) {
	t.Parallel()
	readme := readReadme(t)

	// The sample block is what people copy, so every key has to be in it —
	// mentioning a key in prose somewhere else is not the same as showing it.
	// tab_width was named once, in a sentence about tabs, and absent from the
	// block; a laxer version of this test passed on that.
	sample := between(readme, "```json", "```")
	if sample == "" {
		t.Fatal("the README has no JSON sample config block")
	}

	var missing []string
	for _, key := range configKeys() {
		if !strings.Contains(sample, `"`+key+`"`) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		t.Errorf("the README's sample config omits %d keys: %s",
			len(missing), strings.Join(missing, ", "))
	}
}

// And nothing may be documented that does not exist: advice to set a key that
// is silently ignored is worse than no advice.
func TestConfig_TheREADMEInventsNoKeys(t *testing.T) {
	t.Parallel()
	readme := readReadme(t)

	real := map[string]bool{}
	for _, key := range configKeys() {
		real[key] = true
	}

	// The sample config block is the authoritative listing; anything quoted
	// there has to be a key.
	sample := between(readme, "```json", "```")
	if sample == "" {
		t.Fatal("the README has no JSON sample config block")
	}
	for _, line := range strings.Split(sample, "\n") {
		key, _, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		key = strings.Trim(strings.TrimSpace(key), `"`)
		if key == "" || strings.HasPrefix(key, "{") || strings.HasPrefix(key, "//") {
			continue
		}
		if !real[key] {
			t.Errorf("the README's sample config sets %q, which is not a config key", key)
		}
	}
}

// configKeys is every json tag on Config, which is what load and save read.
func configKeys() []string {
	typ := reflect.TypeOf(Config{})
	keys := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if tag != "" && tag != "-" {
			keys = append(keys, tag)
		}
	}
	return keys
}

func readReadme(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	return string(raw)
}

func between(s, open, close string) string {
	_, rest, ok := strings.Cut(s, open)
	if !ok {
		return ""
	}
	body, _, ok := strings.Cut(rest, close)
	if !ok {
		return ""
	}
	return body
}
