package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/bitwave-io/bitwave-cli/internal/diagnostics"
)

type commandFlagInfo struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Usage     string `json:"usage"`
	Default   string `json:"default,omitempty"`
}

type commandInfo struct {
	Path        string            `json:"path"`
	Use         string            `json:"use"`
	Summary     string            `json:"summary"`
	Description string            `json:"description,omitempty"`
	Aliases     []string          `json:"aliases,omitempty"`
	Example     string            `json:"example,omitempty"`
	Flags       []commandFlagInfo `json:"flags,omitempty"`
	Runnable    bool              `json:"runnable"`
}

func newInfoCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Describe everything the CLI can do",
		Long: `Print a complete catalog of the local-ledger, cloud-workspace, and
organization-platform command tree. Use --json for SDK, agent, and tooling
discovery; the catalog is generated from the live Cobra tree so it cannot
drift from the commands actually registered in this build.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root := cmd.Root()
			root.InitDefaultHelpCmd()
			catalog := collectCommandInfo(root)
			if jsonOutput {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"schemaVersion": "1",
					"cli":           root.Name(),
					"version":       root.Version,
					"commandCount":  len(catalog),
					"commands":      catalog,
				})
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "bitwave v%s — %d registered commands\n\n", root.Version, len(catalog))
			for _, item := range catalog {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-52s %s\n", item.Path, item.Summary)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "\nUse `bitwave help <command>` or `bitwave <command> --help` for flags and examples.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit the complete command catalog as JSON")
	return cmd
}

func collectCommandInfo(root *cobra.Command) []commandInfo {
	items := make([]commandInfo, 0)
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if command != root && (command.IsAvailableCommand() || command.Name() == "help") && !command.Hidden {
			item := commandInfo{
				Path: command.CommandPath(), Use: command.UseLine(), Summary: command.Short,
				Description: strings.TrimSpace(command.Long), Aliases: append([]string(nil), command.Aliases...),
				Example: strings.TrimSpace(command.Example), Runnable: command.Runnable(),
			}
			command.NonInheritedFlags().VisitAll(func(flag *pflag.Flag) {
				if !flag.Hidden {
					item.Flags = append(item.Flags, commandFlagInfo{Name: flag.Name, Shorthand: flag.Shorthand, Usage: flag.Usage, Default: flag.DefValue})
				}
			})
			items = append(items, item)
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items
}

func newLastErrorCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:     "error",
		Aliases: []string{"last-error"},
		Short:   "Show the most recent failed CLI invocation for support",
		Long: `Show a small redacted diagnostic record written after the most recent
failed bitwave invocation. Request query strings and common credential forms
are removed; request and response payloads are never stored.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			record, err := diagnostics.Load()
			if err != nil {
				if errors.Is(err, diagnostics.ErrNoRecordedError) {
					_, writeErr := fmt.Fprintln(cmd.OutOrStdout(), "No CLI error has been recorded.")
					return writeErr
				}
				return err
			}
			if jsonOutput {
				return writeJSON(cmd.OutOrStdout(), record)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Recorded: %s\nCommand:  %s\nError:    %s\n", record.RecordedAt.Format("2006-01-02 15:04:05Z"), record.Command, record.Message)
			if record.HTTPStatus != 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "HTTP:     %s %s (%d)\n", record.HTTPMethod, record.RequestURL, record.HTTPStatus)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}
