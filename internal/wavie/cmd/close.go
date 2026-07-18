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

// newCloseRunStubCmd is a placeholder for wavie close run. The orchestrator
// currently lives inside the bw CLI's internal/cmd package; until it's
// extracted into a shared location, point users at bw.
func newCloseRunStubCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run a close checklist (delegates to `bw close run` for now)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("wavie close run is not yet wired up — use `bw close run` (same flags). The orchestrator will move into a shared package in a follow-up")
		},
	}
}
