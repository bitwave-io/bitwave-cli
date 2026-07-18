package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCloseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close-period orchestrator (run, status, ...)",
	}
	cmd.AddCommand(newCloseRunStubCmd())
	return cmd
}

// newCloseRunStubCmd is a placeholder for bitwave close run; the period-close
// orchestrator has not been ported into this CLI yet.
func newCloseRunStubCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run a close checklist (coming soon)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("`bitwave close run` is not available yet — the period-close orchestrator ships in an upcoming release")
		},
	}
}
