package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWavieServiceFilePaths(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "Users", "test user")
	mac, err := wavieServiceFilePaths("darwin", home)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(mac.definition, filepath.Join("Library", "LaunchAgents", wavieServiceLabel+".plist")) {
		t.Fatalf("mac definition = %q", mac.definition)
	}
	linux, err := wavieServiceFilePaths("linux", home)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(linux.definition, filepath.Join(".config", "systemd", "user", wavieLinuxServiceName)) {
		t.Fatalf("linux definition = %q", linux.definition)
	}
}

func TestRenderLaunchAgentEscapesValuesAndRunsBridge(t *testing.T) {
	plist := renderLaunchAgent(`/Applications/Bitwave & Co/bitwave`, `/Users/test & user`, `/tmp/bridge & log`)
	for _, expected := range []string{
		`<string>/Applications/Bitwave &amp; Co/bitwave</string>`,
		`<string>wavie</string>`,
		`<string>connect</string>`,
		`<string>--service</string>`,
		`<key>RunAtLoad</key><true/>`,
	} {
		if !strings.Contains(plist, expected) {
			t.Fatalf("plist missing %q:\n%s", expected, plist)
		}
	}
}

func TestRenderSystemdUserServiceQuotesPaths(t *testing.T) {
	service := renderSystemdUserService(`/home/test user/bin/bitwave`, `/home/test user`)
	if !strings.Contains(service, `ExecStart="/home/test user/bin/bitwave" --quiet wavie connect --service`) {
		t.Fatalf("unexpected service:\n%s", service)
	}
	if !strings.Contains(service, `Restart=on-failure`) {
		t.Fatalf("service does not restart on failure:\n%s", service)
	}
}

func TestWindowsTaskCommand(t *testing.T) {
	command, err := windowsTaskCommand(`C:\Program Files\Bitwave\bitwave.exe`)
	if err != nil {
		t.Fatal(err)
	}
	want := `"C:\Program Files\Bitwave\bitwave.exe" --quiet wavie connect --service`
	if command != want {
		t.Fatalf("command = %q, want %q", command, want)
	}
}

func TestWavieBridgeServiceUsesHomeDirectory(t *testing.T) {
	workingDirectory, err := wavieBridgeWorkingDirectory(true)
	if err != nil {
		t.Fatal(err)
	}
	if workingDirectory == "" {
		t.Fatal("service working directory is empty")
	}
}

func TestShouldAttemptWavieServiceIsRateLimited(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	if !shouldAttemptWavieService(home, now) {
		t.Fatal("first attempt was rejected")
	}
	marker := filepath.Join(home, ".bitwave", wavieServiceAttemptFile)
	if err := os.Chtimes(marker, now, now); err != nil {
		t.Fatal(err)
	}
	if shouldAttemptWavieService(home, now.Add(time.Hour)) {
		t.Fatal("attempt was not rate limited")
	}
	if err := os.Chtimes(marker, now.Add(-wavieServiceRetryAfter), now.Add(-wavieServiceRetryAfter)); err != nil {
		t.Fatal(err)
	}
	if !shouldAttemptWavieService(home, now) {
		t.Fatal("attempt did not reopen after retry interval")
	}
}
