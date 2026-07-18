// Package orgctx persists the user's "active org" between CLI invocations.
// Cloud-aware commands (e.g. `bw ledger init --cloud`) read this instead of
// taking an --org-id flag on every call.
package orgctx

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoActiveOrg signals that no org has been selected. Callers should print
// the user-facing hint via Hint() and exit non-zero.
var ErrNoActiveOrg = errors.New("no active org")

// Hint is the standard remediation message shown when ErrNoActiveOrg is hit.
const Hint = "No active org. Run `bw org switch` to pick one, or `bw org create` to make a new one."

// Active is the on-disk shape persisted to ~/.bitwave/config.json.
type Active struct {
	OrgID   string `json:"orgId"`
	OrgName string `json:"orgName,omitempty"`
}

type fileShape struct {
	Active *Active `json:"active,omitempty"`
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bitwave", "config.json"), nil
}

// Load reads ~/.bitwave/config.json. Returns nil, ErrNoActiveOrg when no active
// org is set.
func Load() (*Active, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoActiveOrg
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f fileShape
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if f.Active == nil || f.Active.OrgID == "" {
		return nil, ErrNoActiveOrg
	}
	return f.Active, nil
}

// Save writes the active org to ~/.bitwave/config.json.
func Save(a *Active) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f := fileShape{Active: a}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Clear removes the active-org pointer.
func Clear() error {
	return Save(&Active{})
}
