// Package update implements the CLI's update check and self-upgrade.
//
// The check is designed to never slow a command down: the banner only reads
// a small cached state file, and the network refresh (at most once per 24h)
// runs after the command's real work has finished. BITWAVE_NO_UPDATE_CHECK=1
// disables all network checks; dev/snapshot builds never check.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	latestReleaseURL = "https://api.github.com/repos/bitwave-io/bitwave-cli/releases/latest"
	checkInterval    = 24 * time.Hour
	refreshTimeout   = 2500 * time.Millisecond
)

// State is the on-disk cache at ~/.bitwave/update-check.json.
type State struct {
	LatestVersion string    `json:"latestVersion"`
	CheckedAt     time.Time `json:"checkedAt"`
}

func statePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bitwave", "update-check.json"), nil
}

func loadState() (State, bool) {
	var s State
	p, err := statePath()
	if err != nil {
		return s, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return s, false
	}
	if json.Unmarshal(b, &s) != nil {
		return State{}, false
	}
	return s, true
}

func saveState(s State) {
	p, err := statePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(s)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, b, 0o644)
}

// Disabled reports whether update checking is off for this process: opted
// out via env, or a non-release build (anything with a prerelease suffix,
// e.g. 0.1.0-dev or 0.2.1-SNAPSHOT-abc).
func Disabled(currentVersion string) bool {
	if os.Getenv("BITWAVE_NO_UPDATE_CHECK") == "1" {
		return true
	}
	return strings.Contains(currentVersion, "-")
}

// CachedNotice returns a one-line upgrade hint if the cached state knows a
// newer version. It never touches the network.
func CachedNotice(currentVersion string) (string, bool) {
	if Disabled(currentVersion) {
		return "", false
	}
	s, ok := loadState()
	if !ok || !IsNewer(s.LatestVersion, currentVersion) {
		return "", false
	}
	return fmt.Sprintf("bitwave: %s available (current v%s) — run `bitwave upgrade`",
		s.LatestVersion, strings.TrimPrefix(currentVersion, "v")), true
}

// BackgroundRefresh refreshes the cached latest version if it is stale.
// Errors are deliberately swallowed: an offline machine should never see
// update-check noise. Intended to run after a command's real work is done.
func BackgroundRefresh(currentVersion string) {
	if Disabled(currentVersion) {
		return
	}
	if s, ok := loadState(); ok && time.Since(s.CheckedAt) < checkInterval {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	_, _ = Refresh(ctx)
}

// Refresh queries GitHub for the latest release tag and updates the cache.
func Refresh(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release lookup: HTTP %d", resp.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.TagName == "" {
		return "", errors.New("release lookup: empty tag_name")
	}
	saveState(State{LatestVersion: payload.TagName, CheckedAt: time.Now()})
	return payload.TagName, nil
}

// IsNewer reports whether version a is strictly newer than version b.
// Versions are plain X.Y.Z with an optional leading v; anything unparseable
// compares as not-newer (fail safe: no upgrade nag on weird versions).
func IsNewer(a, b string) bool {
	pa, oka := parseVersion(a)
	pb, okb := parseVersion(b)
	if !oka || !okb {
		return false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// Reject prerelease/build suffixes rather than misparse them.
	if strings.ContainsAny(v, "-+") {
		return out, false
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// InstallMethod classifies how the running binary was installed, from its
// (symlink-resolved) path.
type InstallMethod string

const (
	MethodNpm    InstallMethod = "npm"
	MethodBrew   InstallMethod = "brew"
	MethodDirect InstallMethod = "direct"
)

// DetectInstallMethod classifies an executable path. Callers should pass a
// symlink-resolved path (Homebrew links $(brew --prefix)/bin/bitwave into
// the Caskroom; npm links into node_modules).
func DetectInstallMethod(exePath string) InstallMethod {
	// Normalize both separator styles (ToSlash only handles the host OS's).
	p := strings.ReplaceAll(exePath, `\`, "/")
	switch {
	case strings.Contains(p, "/node_modules/"):
		return MethodNpm
	case strings.Contains(p, "/Caskroom/"),
		strings.Contains(p, "/Cellar/"),
		strings.Contains(p, "/homebrew/"),
		strings.Contains(p, "/.linuxbrew/"):
		return MethodBrew
	default:
		return MethodDirect
	}
}
