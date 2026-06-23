package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/bwx/config"
	"github.com/bitwave-io/bitwave-cli/internal/bwx/store"
	"github.com/bitwave-io/bitwave-cli/internal/bwx/workspaces"
)

func newInitCmd() *cobra.Command {
	var (
		cloud        bool
		name         string
		baseCurrency string
		dirFlag      string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a bwx workspace in the current (or --dir) directory",
		Long: `bwx init creates a .bwx.toml marker in the current directory (or --dir),
plus empty accounts.ledger / prices.ledger files for local mode. This must
run BEFORE any other bwx command — most commands fail with "not a bwx
workspace" until a .bwx.toml exists at or above the cwd.

The workspace lives where you run init. Run it from inside the directory
you want to use (e.g. ` + "`cd ~/my-expenses && bwx init`" + `), or pass --dir to
scaffold somewhere else. Workspace name defaults to the directory basename.

Local mode (default):
  bwx init [--name N] [--base-currency USD]
    Writes .bwx.toml plus empty accounts.ledger / prices.ledger.

Cloud mode (--cloud):
  bwx init --cloud --name N [--base-currency USD]
    Creates a LedgerWorkspace under the active org and binds the cwd to it.
    Requires ` + "`bwx auth login`" + ` and an active org (` + "`bwx org use`" + `).

Examples:
  cd ~/my-expenses && bwx init
  bwx init --dir ./jan-2026 --name jan-expenses
  bwx init --base-currency EUR
  bwx init --cloud --name acme-fy26`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := dirFlag
			if dir == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				dir = cwd
			}
			abs, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if baseCurrency == "" {
				baseCurrency = "USD"
			}
			if cloud {
				if name == "" {
					return fmt.Errorf("--name is required for --cloud")
				}
				return runInitCloud(abs, name, baseCurrency)
			}
			if name == "" {
				name = filepath.Base(abs)
			}
			return runInitLocal(abs, name, baseCurrency)
		},
	}
	cmd.Flags().BoolVar(&cloud, "cloud", false, "Create a cloud-backed workspace under the active org")
	cmd.Flags().StringVar(&name, "name", "", "Workspace name (required for --cloud; defaults to dir name otherwise)")
	cmd.Flags().StringVar(&baseCurrency, "base-currency", "USD", "Workspace base currency")
	cmd.Flags().StringVar(&dirFlag, "dir", "", "Workspace directory (defaults to cwd)")
	return cmd
}

func runInitLocal(dir, name, baseCurrency string) error {
	if _, err := store.InitLocal(dir, name, baseCurrency); err != nil {
		return err
	}
	fmt.Printf("Initialized local workspace at %s\n", dir)
	fmt.Println("Next:")
	fmt.Println("  bwx expense new --report <id> --amount 10 --account Expenses:Meals")
	fmt.Println("  bwx je new   (raw double-entry)   |   bwx --help   (all commands)")
	return nil
}

func runInitCloud(dir, name, baseCurrency string) error {
	if _, err := os.Stat(filepath.Join(dir, config.FileName)); err == nil {
		return fmt.Errorf("workspace already initialized at %s", dir)
	}
	active, err := requireActiveOrg()
	if err != nil {
		return err
	}
	c := workspaces.New(resolveGLBaseURL(), active.OrgID, makeOrgTokenResolver(active.OrgID))
	id, err := c.CreateWorkspace(workspaces.CreateWorkspaceRequest{
		Name:         name,
		BaseCurrency: baseCurrency,
	})
	if err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	cfg := &config.Config{
		Mode:         config.ModeCloud,
		Name:         name,
		BaseCurrency: baseCurrency,
		OrgId:        active.OrgID,
		WorkspaceId:  id,
	}
	if err := config.Save(dir, cfg); err != nil {
		return fmt.Errorf("save .bwx.toml: %w", err)
	}
	fmt.Printf("Created cloud workspace: %s (%s)\n", name, id)
	fmt.Printf("Bound %s to it.\n", dir)
	return nil
}
