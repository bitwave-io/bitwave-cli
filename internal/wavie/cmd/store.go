package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/bitwave-io/bitwave-cli/internal/wavie/config"
	"github.com/bitwave-io/bitwave-cli/internal/wavie/store"
)

// resolveStore builds a wavie store rooted at the cwd's .wavie.toml. Most ledger
// commands call this rather than poking at the config directly.
func resolveStore(ctx context.Context) (store.Store, *config.Config, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, "", err
	}
	dir, err := config.Find(cwd)
	if err != nil {
		if errors.Is(err, config.ErrNotAWorkspace) {
			return nil, nil, "", fmt.Errorf("%w — run `wavie init` here (or `cd` to an existing workspace) before this command", err)
		}
		return nil, nil, "", err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, nil, "", err
	}
	switch cfg.Mode {
	case config.ModeLocal:
		ws, err := store.OpenLocal(dir)
		if err != nil {
			return nil, nil, "", err
		}
		return ws, cfg, dir, nil
	case config.ModeCloud:
		if cfg.OrgId == "" || cfg.WorkspaceId == "" {
			return nil, nil, "", fmt.Errorf("cloud workspace missing org_id or workspace_id in %s", config.FileName)
		}
		c := store.NewCloud(resolveGLBaseURL(), cfg.OrgId, cfg.WorkspaceId, makeOrgTokenResolver(cfg.OrgId))
		return c, cfg, dir, nil
	default:
		return nil, nil, "", fmt.Errorf("unknown workspace mode %q in %s", cfg.Mode, config.FileName)
	}
}

// resolveJournal returns the journal id a write should land in, applying:
//   - explicit non-empty: returned as-is (auto-created if missing)
//   - cfg.DefaultJournal when set
//   - 0 journals: auto-create "default"
//   - 1 journal: return it
//   - 2+ journals: error suggesting --journal
func resolveJournal(ctx context.Context, s store.Store, cfg *config.Config, explicit string) (string, error) {
	if explicit != "" {
		if err := s.EnsureJournal(ctx, explicit); err != nil {
			return "", err
		}
		return explicit, nil
	}
	if cfg.DefaultJournal != "" {
		if err := s.EnsureJournal(ctx, cfg.DefaultJournal); err != nil {
			return "", err
		}
		return cfg.DefaultJournal, nil
	}
	ids, err := s.Journals(ctx)
	if err != nil {
		return "", err
	}
	switch len(ids) {
	case 0:
		id := store.DefaultJournal
		if err := s.EnsureJournal(ctx, id); err != nil {
			return "", err
		}
		return id, nil
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("multiple journals in workspace; pass --journal to disambiguate (have: %v)", ids)
	}
}
