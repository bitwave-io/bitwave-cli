package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/bwx/config"
	"github.com/bitwave-io/bitwave-cli/internal/bwx/store"
	"github.com/bitwave-io/bitwave-cli/internal/bwx/workspaces"
)

func newJournalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "journal",
		Short: "Manage journals within the current workspace",
		Long: `Workspaces hold one or more journals. In local mode each journal is a
<id>.journal file in the workspace dir; in cloud mode it's a row in
gl-svc.

` + "`bwx journal use`" + ` records a default journal id in .bwx.toml so
` + "`bwx je new`" + ` doesn't have to keep passing --journal.`,
	}
	cmd.AddCommand(newJournalListCmd())
	cmd.AddCommand(newJournalNewCmd())
	cmd.AddCommand(newJournalUseCmd())
	return cmd
}

func newJournalListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List journals in the current workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, dir, err := loadCwdConfig()
			if err != nil {
				return err
			}
			ids, names, err := listJournals(cfg, dir)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				fmt.Println("(no journals)")
				return nil
			}
			for i, id := range ids {
				marker := "  "
				if id == cfg.DefaultJournal {
					marker = "* "
				}
				if names[i] != "" && names[i] != id {
					fmt.Printf("%s%-24s  %s\n", marker, id, names[i])
				} else {
					fmt.Printf("%s%s\n", marker, id)
				}
			}
			return nil
		},
	}
}

func newJournalNewCmd() *cobra.Command {
	var name, description string
	cmd := &cobra.Command{
		Use:   "new <id>",
		Short: "Create a journal in the current workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("journal id is required")
			}
			cfg, dir, err := loadCwdConfig()
			if err != nil {
				return err
			}
			switch cfg.Mode {
			case config.ModeLocal:
				ws, err := store.OpenLocal(dir)
				if err != nil {
					return err
				}
				if err := ws.EnsureJournal(cmd.Context(), id); err != nil {
					return err
				}
				fmt.Printf("Created journal: %s\n", id)
				return nil
			case config.ModeCloud:
				if cfg.OrgId == "" || cfg.WorkspaceId == "" {
					return fmt.Errorf(".bwx.toml is missing org_id or workspace_id")
				}
				c := workspaces.New(resolveGLBaseURL(), cfg.OrgId, makeOrgTokenResolver(cfg.OrgId))
				if name == "" {
					name = titleCase(strings.ReplaceAll(id, "-", " "))
				}
				newId, err := c.CreateJournal(cfg.WorkspaceId, workspaces.CreateJournalRequest{
					Id:          id,
					Name:        name,
					Description: description,
				})
				if err != nil {
					return fmt.Errorf("create journal: %w", err)
				}
				fmt.Printf("Created journal: %s\n", newId)
				return nil
			default:
				return fmt.Errorf("unknown workspace mode: %s", cfg.Mode)
			}
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Journal display name (cloud mode; defaults to title-cased id)")
	cmd.Flags().StringVar(&description, "description", "", "Journal description (cloud mode)")
	return cmd
}

func newJournalUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <id>",
		Short: "Set the default journal for `bwx je new` and other writes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			cfg, dir, err := loadCwdConfig()
			if err != nil {
				return err
			}
			ids, _, err := listJournals(cfg, dir)
			if err != nil {
				return err
			}
			found := false
			for _, j := range ids {
				if j == id {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("journal %s does not exist (run `bwx journal new %s`)", id, id)
			}
			cfg.DefaultJournal = id
			if err := config.Save(dir, cfg); err != nil {
				return err
			}
			fmt.Printf("Default journal: %s\n", id)
			return nil
		},
	}
}

// titleCase upper-cases the first rune of every space-separated word. Avoids
// strings.Title (deprecated) without dragging in x/text/cases for one helper.
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// listJournals returns journal ids and parallel display names for the active
// workspace. Cloud mode hits gl-svc; local mode reads files.
func listJournals(cfg *config.Config, dir string) ([]string, []string, error) {
	switch cfg.Mode {
	case config.ModeLocal:
		ws, err := store.OpenLocal(dir)
		if err != nil {
			return nil, nil, err
		}
		ids, err := ws.JournalIds()
		if err != nil {
			return nil, nil, err
		}
		names := make([]string, len(ids))
		return ids, names, nil
	case config.ModeCloud:
		if cfg.OrgId == "" || cfg.WorkspaceId == "" {
			return nil, nil, fmt.Errorf(".bwx.toml is missing org_id or workspace_id")
		}
		c := workspaces.New(resolveGLBaseURL(), cfg.OrgId, makeOrgTokenResolver(cfg.OrgId))
		js, err := c.ListJournals(cfg.WorkspaceId)
		if err != nil {
			return nil, nil, fmt.Errorf("list journals: %w", err)
		}
		ids := make([]string, len(js))
		names := make([]string, len(js))
		for i, j := range js {
			ids[i] = j.Id
			names[i] = j.Name
		}
		return ids, names, nil
	default:
		return nil, nil, fmt.Errorf("unknown workspace mode: %s", cfg.Mode)
	}
}
