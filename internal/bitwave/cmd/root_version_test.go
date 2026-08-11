package cmd

import (
	"runtime/debug"
	"testing"
)

func TestResolveBuildVersion(t *testing.T) {
	tests := []struct {
		name     string
		fallback string
		info     *debug.BuildInfo
		ok       bool
		want     string
	}{
		{name: "missing build info", fallback: developmentVersion, want: developmentVersion},
		{name: "source checkout", fallback: developmentVersion, info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, ok: true, want: developmentVersion},
		{name: "release module", fallback: developmentVersion, info: &debug.BuildInfo{Main: debug.Module{Version: "v0.3.0"}}, ok: true, want: "0.3.0"},
		{name: "pseudo version", fallback: developmentVersion, info: &debug.BuildInfo{Main: debug.Module{Version: "v0.3.1-0.20260812000000-abcdef123456"}}, ok: true, want: "0.3.1-0.20260812000000-abcdef123456"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveBuildVersion(test.fallback, test.info, test.ok); got != test.want {
				t.Fatalf("resolveBuildVersion() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveRuntimeVersionKeepsLinkerValue(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v0.2.5-0.20260812000000-abcdef123456"}}
	if got := resolveRuntimeVersion("0.3.0-dev", info, true); got != "0.3.0-dev" {
		t.Fatalf("resolveRuntimeVersion() = %q, want %q", got, "0.3.0-dev")
	}
}
