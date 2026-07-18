// Package config handles the per-workspace .wavie.toml file that records
// whether a workspace is local or cloud-backed.
//
// A wavie workspace is a directory containing:
//   - .wavie.toml        — this config (mode, currency, optional cloud ids)
//   - <name>.journal   — one or more journal files (local mode)
//   - accounts.ledger  — account declarations (local mode)
//   - prices.ledger    — price observations (local mode)
//
// In cloud mode the journal/account/price data lives in gl-svc; the local
// directory still holds .wavie.toml as the workspace marker so cwd-aware
// commands can find it.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// FileName is the workspace marker file.
const FileName = ".wavie.toml"

// Mode is "local" or "cloud".
type Mode string

const (
	ModeLocal Mode = "local"
	ModeCloud Mode = "cloud"
)

// Config is the on-disk per-workspace state.
type Config struct {
	Mode           Mode   `toml:"mode"`
	Name           string `toml:"name,omitempty"`
	BaseCurrency   string `toml:"base_currency"`
	OrgId          string `toml:"org_id,omitempty"`
	WorkspaceId    string `toml:"workspace_id,omitempty"`
	DefaultJournal string `toml:"default_journal,omitempty"`
}

// ErrNotAWorkspace is returned by Find/Load when no .wavie.toml is present.
var ErrNotAWorkspace = errors.New("not a wavie workspace (no .wavie.toml)")

// Find walks up from start looking for a .wavie.toml. Returns the workspace
// directory (the one containing the file). Returns ErrNotAWorkspace if none
// is found before the filesystem root.
func Find(start string) (string, error) {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, FileName)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotAWorkspace
		}
		dir = parent
	}
}

// Load reads <dir>/.wavie.toml.
func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotAWorkspace
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.BaseCurrency == "" {
		c.BaseCurrency = "USD"
	}
	return &c, nil
}

// Save writes the config to <dir>/.wavie.toml with 0644 perms.
func Save(dir string, c *Config) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, FileName))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return toml.NewEncoder(f).Encode(c)
}
