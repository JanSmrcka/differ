package cmd

import (
	"runtime/debug"
	"testing"
)

// The version Cobra prints has to be the one init resolved.
//
// It was set in rootCmd's composite literal, which is evaluated when the
// package variable is initialised — before init() runs. So the
// debug.ReadBuildInfo fallback assigned to `version` and Cobra kept the "dev"
// it had already captured: `go install github.com/jansmrcka/differ@latest`,
// which is what the README tells people to run, reported "dev" forever. Builds
// through the Makefile were fine, because ldflags set the variable before
// either ran.
func TestVersion_CobraReportsWhatWasResolved(t *testing.T) {
	original := version
	defer setVersion(original)

	setVersion("v9.9.9-probe")
	if rootCmd.Version != "v9.9.9-probe" {
		t.Errorf("rootCmd reports %q after the version was set to %q",
			rootCmd.Version, "v9.9.9-probe")
	}
}

// resolveVersion is where the two sources are reconciled, so it is the thing
// worth testing: ldflags win, and the module version is the fallback.
func TestResolveVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		ldflags string
		info    *debug.BuildInfo
		ok      bool
		want    string
	}{
		{
			name:    "ldflags win over build info",
			ldflags: "v1.2.3",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v0.0.1"}},
			ok:      true,
			want:    "v1.2.3",
		},
		{
			name:    "go install reports the module version",
			ldflags: "dev",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v1.6.0"}},
			ok:      true,
			want:    "v1.6.0",
		},
		{
			name:    "a local build stays dev rather than claiming (devel)",
			ldflags: "dev",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			ok:      true,
			want:    "dev",
		},
		{
			name:    "no build info at all",
			ldflags: "dev",
			info:    nil,
			ok:      false,
			want:    "dev",
		},
		{
			name:    "build info with an empty version",
			ldflags: "dev",
			info:    &debug.BuildInfo{Main: debug.Module{Version: ""}},
			ok:      true,
			want:    "dev",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveVersion(tc.ldflags, tc.info, tc.ok); got != tc.want {
				t.Errorf("resolveVersion(%q, …) = %q, want %q", tc.ldflags, got, tc.want)
			}
		})
	}
}
