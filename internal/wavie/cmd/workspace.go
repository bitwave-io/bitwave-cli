package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/wavie/config"
	"github.com/bitwave-io/bitwave-cli/internal/wavie/workspaces"
	"github.com/bitwave-io/bitwave-cli/internal/wavie/workspaceshare"
)

func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Manage cloud workspaces in the active org",
		Long: `wavie workspace lists, creates, and switches between cloud workspaces in
the active org. The selection is recorded in the cwd's .wavie.toml so
later commands (wavie je, wavie bal, ...) target the right workspace.`,
	}
	cmd.AddCommand(newWorkspaceListCmd())
	cmd.AddCommand(newWorkspaceUseCmd())
	cmd.AddCommand(newWorkspaceCurrentCmd())
	cmd.AddCommand(newWorkspaceCreateCmd())
	cmd.AddCommand(newWorkspaceAdoptCmd())
	return cmd
}

// newWorkspaceAdoptCmd accepts a pending shared workspace from gl-svc. The
// recipient must be logged in (PKCE or BITWAVE_TOKEN); auth is forwarded as
// the bearer token. gl-svc starts a Temporal workflow that drains the
// stashed zip into real ledger rows under the recipient's default org.
//
// The command returns as soon as the workflow is scheduled — it doesn't wait
// for hydration to finish. Hydration is idempotent and the row's Status
// flips back to active when it completes; `wavie workspace list` will then
// show the newly-adopted workspace.
func newWorkspaceAdoptCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "adopt <workspaceId>",
		Short: "Accept a shared workspace into the active org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workspaceId := args[0]
			active, err := requireActiveOrg()
			if err != nil {
				return err
			}
			c := workspaceshare.New(resolveGLBaseURL(), makeOrgTokenResolver(active.OrgID))
			resp, err := c.Adopt(cmd.Context(), workspaceId, name)
			if err != nil {
				return fmt.Errorf("adopt workspace: %w", err)
			}
			fmt.Printf("Adopted workspace %s\n", resp.WorkspaceId)
			fmt.Printf("  Workflow:    %s\n", resp.WorkflowId)
			fmt.Printf("  WorkflowRun: %s\n", resp.WorkflowRunId)
			fmt.Printf("Hydration runs asynchronously; use `wavie workspace list` to see when it appears.\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Rename the adopted workspace (defaults to the shared name)")
	return cmd
}

func newWorkspaceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List cloud workspaces in the active org",
		RunE: func(cmd *cobra.Command, _ []string) error {
			active, err := requireActiveOrg()
			if err != nil {
				return err
			}
			c := workspaces.New(resolveGLBaseURL(), active.OrgID, makeOrgTokenResolver(active.OrgID))
			ws, err := c.ListWorkspaces()
			if err != nil {
				return fmt.Errorf("list workspaces: %w", err)
			}
			if len(ws) == 0 {
				fmt.Println("(no workspaces) — run `wavie init --cloud --name <n>` to create one")
				return nil
			}
			currentId := loadCurrentWorkspaceId()
			for _, w := range ws {
				marker := "  "
				if w.Id == currentId {
					marker = "* "
				}
				fmt.Printf("%s%-32s  %s  (%s)\n", marker, w.Id, w.Name, w.BaseCurrency)
			}
			return nil
		},
	}
}

func newWorkspaceCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Print the workspace recorded in the cwd's .wavie.toml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := loadCwdConfig()
			if err != nil {
				return err
			}
			if cfg.Mode != config.ModeCloud {
				return fmt.Errorf("workspace at cwd is in %s mode, not cloud", cfg.Mode)
			}
			if cfg.WorkspaceId == "" {
				return fmt.Errorf("no workspace_id in .wavie.toml — run `wavie workspace use`")
			}
			fmt.Println(cfg.WorkspaceId)
			return nil
		},
	}
}

func newWorkspaceUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use [workspaceId]",
		Short: "Point the cwd .wavie.toml at a cloud workspace. Bare invocation shows a picker.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			active, err := requireActiveOrg()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			c := workspaces.New(resolveGLBaseURL(), active.OrgID, makeOrgTokenResolver(active.OrgID))

			var picked workspaces.Workspace
			if len(args) == 1 {
				ws, err := c.ListWorkspaces()
				if err != nil {
					return fmt.Errorf("list workspaces: %w", err)
				}
				for _, w := range ws {
					if w.Id == args[0] {
						picked = w
						break
					}
				}
				if picked.Id == "" {
					return fmt.Errorf("workspace %s not found in org %s", args[0], active.OrgID)
				}
			} else {
				ws, err := c.ListWorkspaces()
				if err != nil {
					return fmt.Errorf("list workspaces: %w", err)
				}
				if len(ws) == 0 {
					fmt.Println("No workspaces. Run: wavie init --cloud --name <name>")
					return nil
				}
				fmt.Println("Pick a workspace:")
				for i, w := range ws {
					fmt.Printf("  [%d] %s  (%s)\n", i+1, w.Name, w.Id)
				}
				fmt.Print("> ")
				rdr := bufio.NewReader(os.Stdin)
				line, _ := rdr.ReadString('\n')
				n, err := strconv.Atoi(strings.TrimSpace(line))
				if err != nil || n < 1 || n > len(ws) {
					return fmt.Errorf("invalid selection")
				}
				picked = ws[n-1]
			}

			cfg, dir, err := loadCwdConfig()
			if err != nil && !errors.Is(err, config.ErrNotAWorkspace) {
				return err
			}
			if cfg == nil {
				cfg = &config.Config{}
				dir = cwd
			}
			cfg.Mode = config.ModeCloud
			cfg.OrgId = active.OrgID
			cfg.WorkspaceId = picked.Id
			if cfg.Name == "" {
				cfg.Name = picked.Name
			}
			if cfg.BaseCurrency == "" {
				cfg.BaseCurrency = picked.BaseCurrency
			}
			if err := config.Save(dir, cfg); err != nil {
				return err
			}
			fmt.Printf("Active workspace: %s (%s)\n", picked.Name, picked.Id)
			return nil
		},
	}
}

func newWorkspaceCreateCmd() *cobra.Command {
	var name, baseCurrency string
	cmd := &cobra.Command{
		Use:   "create --name <n> [--base-currency USD]",
		Short: "Create a workspace in the active org without binding cwd",
		Long: `wavie workspace create makes an empty cloud workspace under the active
org. It does NOT touch cwd's .wavie.toml — for that use ` + "`wavie init --cloud`" + `
which creates the workspace and binds the directory in one step.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			if baseCurrency == "" {
				baseCurrency = "USD"
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
			fmt.Printf("Created workspace: %s (%s)\n", name, id)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Workspace name")
	cmd.Flags().StringVar(&baseCurrency, "base-currency", "USD", "Workspace base currency")
	return cmd
}

// loadCwdConfig finds the nearest .wavie.toml from cwd and returns it.
func loadCwdConfig() (*config.Config, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	dir, err := config.Find(cwd)
	if err != nil {
		if errors.Is(err, config.ErrNotAWorkspace) {
			return nil, "", fmt.Errorf("%w — run `wavie init` here (or `cd` to an existing workspace) before this command", err)
		}
		return nil, "", err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, dir, err
	}
	return cfg, dir, nil
}

// loadCurrentWorkspaceId returns "" when cwd is not bound to a cloud workspace.
func loadCurrentWorkspaceId() string {
	cfg, _, err := loadCwdConfig()
	if err != nil {
		return ""
	}
	if cfg.Mode != config.ModeCloud {
		return ""
	}
	return cfg.WorkspaceId
}
