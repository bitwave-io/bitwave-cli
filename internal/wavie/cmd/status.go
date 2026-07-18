package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show workspace, org, and identity in one line",
		Long: `Print the wavie operating context — workspace dir + mode, active org,
and identity (or anonymous). This is the same banner that prints to stderr
before every command unless --quiet is set or WAVIE_QUIET=1.

Useful as a first command for agents to orient themselves.`,
		Run: func(cmd *cobra.Command, _ []string) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), describeStatus())
		},
	}
}
