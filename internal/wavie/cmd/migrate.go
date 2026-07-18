package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/wavie/config"
	"github.com/bitwave-io/bitwave-cli/internal/wavie/store"
	"github.com/bitwave-io/bitwave-cli/internal/wavie/workspaces"
)

func newMigrateCmd() *cobra.Command {
	var name, invite string
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Push a local workspace up to a new cloud workspace under the active org",
		Long: `Migrate the cwd's local workspace to gl-svc:
  1. Create a new LedgerWorkspace in the active org
  2. Push every journal's entries (and the accounts/prices) into it
  3. Rewrite .wavie.toml to mode=cloud + the new ids
  4. Move local *.ledger / *.journal files to *.bak for rollback

--invite <email> (NOT YET IMPLEMENTED) would target an existing user's org
via the wavie delegation flow. For now this errors out — pass --invite once
the server-side delegation endpoint lands.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if invite != "" {
				return fmt.Errorf("--invite requires the wavie delegation flow; not yet implemented")
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			dir, err := config.Find(cwd)
			if err != nil {
				return err
			}
			cfg, err := config.Load(dir)
			if err != nil {
				return err
			}
			if cfg.Mode != config.ModeLocal {
				return fmt.Errorf("only local workspaces can be migrated (current mode: %s)", cfg.Mode)
			}

			active, err := requireActiveOrg()
			if err != nil {
				return err
			}

			ls, err := store.OpenLocal(dir)
			if err != nil {
				return err
			}
			proj, err := ls.Project(cmd.Context())
			if err != nil {
				return err
			}
			for i, e := range proj.Entries {
				if !e.IsBalanced(cfg.BaseCurrency) {
					return fmt.Errorf("entry %d (%s) does not balance — fix locally before migrating", i, e.Date.Format("2006-01-02"))
				}
			}

			workspaceName := name
			if workspaceName == "" {
				workspaceName = cfg.Name
			}
			if workspaceName == "" {
				workspaceName = filepath.Base(dir)
			}

			wc := workspaces.New(resolveGLBaseURL(), active.OrgID, makeOrgTokenResolver(active.OrgID))
			wsId, err := wc.CreateWorkspace(workspaces.CreateWorkspaceRequest{
				Name:         workspaceName,
				BaseCurrency: cfg.BaseCurrency,
			})
			if err != nil {
				return fmt.Errorf("create cloud workspace: %w", err)
			}

			// Push each local journal as its own cloud journal so the multi-
			// journal layout survives the migration.
			cs := store.NewCloud(resolveGLBaseURL(), active.OrgID, wsId, makeOrgTokenResolver(active.OrgID))
			journalIds, err := ls.JournalIds()
			if err != nil {
				return err
			}
			if len(journalIds) == 0 {
				journalIds = []string{store.DefaultJournal}
			}
			for _, jid := range journalIds {
				if err := cs.EnsureJournal(cmd.Context(), jid); err != nil {
					return fmt.Errorf("ensure cloud journal %s: %w", jid, err)
				}
			}

			// Accounts/prices live at the workspace level — push the whole
			// project once but routed through the first journal for entries.
			// Then the remaining journals get only their own entries.
			if len(journalIds) == 1 {
				if err := cs.Import(cmd.Context(), journalIds[0], proj); err != nil {
					return fmt.Errorf("import to cloud: %w", err)
				}
			} else {
				wsLevel := *proj
				wsLevel.Entries = nil
				if err := cs.Import(cmd.Context(), journalIds[0], &wsLevel); err != nil {
					return fmt.Errorf("import accounts/prices: %w", err)
				}
				for _, jid := range journalIds {
					ents, err := ls.ParseJournalEntries(jid)
					if err != nil {
						return fmt.Errorf("parse %s: %w", jid, err)
					}
					if len(ents) == 0 {
						continue
					}
					sub := *proj
					sub.Accounts = nil
					sub.Prices = nil
					sub.Entries = ents
					if err := cs.Import(cmd.Context(), jid, &sub); err != nil {
						return fmt.Errorf("import journal %s: %w", jid, err)
					}
				}
			}

			cfg.Mode = config.ModeCloud
			cfg.OrgId = active.OrgID
			cfg.WorkspaceId = wsId
			cfg.Name = workspaceName
			if err := config.Save(dir, cfg); err != nil {
				return fmt.Errorf("update %s: %w", config.FileName, err)
			}

			// Move local files aside.
			renameToBak(filepath.Join(dir, store.AccountsFile))
			renameToBak(filepath.Join(dir, store.PricesFile))
			for _, jid := range journalIds {
				renameToBak(filepath.Join(dir, jid+store.JournalExt))
			}

			fmt.Printf("Migrated to cloud workspace %s (org %s)\n", wsId, active.OrgID)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Cloud workspace name (defaults to local workspace name)")
	cmd.Flags().StringVar(&invite, "invite", "", "Delegate ownership to this email (delegation flow — not yet implemented)")
	return cmd
}

func renameToBak(p string) {
	if _, err := os.Stat(p); err == nil {
		_ = os.Rename(p, p+".bak")
	}
}
