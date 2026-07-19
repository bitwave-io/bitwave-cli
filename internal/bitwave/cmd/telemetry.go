package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/telemetry"
)

func newTelemetryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Show or change anonymous usage telemetry",
		Long: `bitwave collects anonymous usage telemetry to understand which commands
humans and agents actually use: command names, flag NAMES (never values),
version, OS/arch, install channel, duration, and success/error class. It
never collects payees, amounts, accounts, file paths, addresses, or any
ledger data. Events batch locally and post to ` + telemetry.Endpoint() + `.

Ways to opt out (no prompt is ever shown):
  bitwave telemetry disable      persisted, also wipes unsent events
  BITWAVE_TELEMETRY=0            per-process env
  DO_NOT_TRACK=1                 cross-tool convention

Full details: https://github.com/bitwave-io/bitwave-cli/blob/main/docs/TELEMETRY.md`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTelemetryStatus(cmd)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show whether telemetry is enabled and why",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTelemetryStatus(cmd)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "disable",
		Short: "Turn telemetry off (persisted; wipes unsent events)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			telemetry.SetDisabled(true)
			fmt.Fprintln(cmd.OutOrStdout(), "telemetry disabled — unsent events wiped, nothing will be recorded or sent")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "enable",
		Short: "Turn telemetry back on",
		RunE: func(cmd *cobra.Command, _ []string) error {
			telemetry.SetDisabled(false)
			fmt.Fprintln(cmd.OutOrStdout(), "telemetry enabled")
			return nil
		},
	})
	return cmd
}

func runTelemetryStatus(cmd *cobra.Command) error {
	d := telemetry.Decide(Version)
	state := "disabled"
	if d.Enabled {
		state = "enabled"
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "telemetry: %s (%s)\n", state, d.Reason)
	fmt.Fprintf(out, "endpoint:  %s\n", telemetry.Endpoint())
	if id := telemetry.AnonymousIdForStatus(); id != "" {
		fmt.Fprintf(out, "anon id:   %s\n", id)
	}
	fmt.Fprintf(out, "unsent:    %d event(s) spooled locally\n", telemetry.SpoolCount())
	return nil
}
