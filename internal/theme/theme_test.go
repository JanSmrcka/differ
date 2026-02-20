package theme

import (
	"reflect"
	"strings"
	"testing"
)

func TestThemes_MapCompleteness(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"dark", "light"} {
		if _, ok := Themes[name]; !ok {
			t.Errorf("Themes map missing %q", name)
		}
	}
}

func checkNonEmpty(t *testing.T, th Theme, label string) {
	t.Helper()
	v := reflect.ValueOf(th)
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		name := typ.Field(i).Name
		if field.Kind() == reflect.String && field.String() == "" {
			t.Errorf("%s.%s is empty", label, name)
		}
	}
}

func TestDarkTheme_NonEmpty(t *testing.T) {
	t.Parallel()
	checkNonEmpty(t, DarkTheme(), "DarkTheme")
}

func TestLightTheme_NonEmpty(t *testing.T) {
	t.Parallel()
	checkNonEmpty(t, LightTheme(), "LightTheme")
}

func TestDarkTheme_ChromaStyle(t *testing.T) {
	t.Parallel()
	th := DarkTheme()
	if !strings.Contains(th.ChromaStyle, "dark") {
		t.Errorf("dark theme ChromaStyle=%q, expected to contain 'dark'", th.ChromaStyle)
	}
}

func TestLightTheme_ChromaStyle(t *testing.T) {
	t.Parallel()
	th := LightTheme()
	if th.ChromaStyle == "" {
		t.Error("light theme ChromaStyle should not be empty")
	}
}

func TestThemes_DarkEqualsFunction(t *testing.T) {
	t.Parallel()
	if !reflect.DeepEqual(Themes["dark"], DarkTheme()) {
		t.Error("Themes[dark] != DarkTheme()")
	}
}

func TestThemes_LightEqualsFunction(t *testing.T) {
	t.Parallel()
	if !reflect.DeepEqual(Themes["light"], LightTheme()) {
		t.Error("Themes[light] != LightTheme()")
	}
}
