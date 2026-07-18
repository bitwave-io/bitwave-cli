package update

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.2.1", "0.2.0", true},
		{"0.2.1", "v0.2.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.2.0", "v0.2.1", false},
		{"v1.0.0", "0.9.9", true},
		{"v0.10.0", "v0.9.0", true}, // numeric, not lexicographic
		{"v0.2.1-SNAPSHOT-abc", "0.2.0", false},
		{"v0.2.1", "0.1.0-dev", false},
		{"garbage", "0.1.0", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.a, c.b); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestDisabled(t *testing.T) {
	t.Setenv("BITWAVE_NO_UPDATE_CHECK", "")
	if Disabled("0.2.0") {
		t.Error("release build should not be disabled")
	}
	if !Disabled("0.1.0-dev") {
		t.Error("dev build should be disabled")
	}
	if !Disabled("0.2.1-SNAPSHOT-abc") {
		t.Error("snapshot build should be disabled")
	}
	t.Setenv("BITWAVE_NO_UPDATE_CHECK", "1")
	if !Disabled("0.2.0") {
		t.Error("env opt-out should disable")
	}
}

func TestDetectInstallMethod(t *testing.T) {
	cases := []struct {
		path string
		want InstallMethod
	}{
		{"/Users/x/.nvm/versions/node/v20/lib/node_modules/@bitwave-io/bitwave-darwin-arm64/bitwave", MethodNpm},
		{"/opt/homebrew/Caskroom/bitwave/0.2.0/bitwave", MethodBrew},
		{"/usr/local/Cellar/bitwave/0.2.0/bin/bitwave", MethodBrew},
		{"/home/linuxbrew/.linuxbrew/bin/bitwave", MethodBrew},
		{"/Users/x/.local/bin/bitwave", MethodDirect},
		{"/usr/local/bin/bitwave", MethodDirect},
		{`C:\Users\x\AppData\npm\node_modules\@bitwave-io\bitwave-win32-x64\bitwave.exe`, MethodNpm},
	}
	for _, c := range cases {
		if got := DetectInstallMethod(c.path); got != c.want {
			t.Errorf("DetectInstallMethod(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
