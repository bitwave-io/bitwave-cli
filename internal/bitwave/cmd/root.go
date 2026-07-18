// Package cmd defines the bitwave CLI command tree.
//
// bitwave is a reshape of bw focused on plain-text accounting workflows that an
// agent can drive end-to-end, plus delegation-friendly auth modalities. The
// surface is intentionally narrower than bw: ledger verbs are top-level (bal,
// reg, print, ...) and discovery-driven commands are not exposed.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/auth"
	"github.com/bitwave-io/bitwave-cli/internal/update"
)

// Version is set at build time via -ldflags.
var Version = "0.1.0-dev"

// NewRootCmd builds the bitwave root command tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "bitwave",
		Short: "Bitwave plain-text accounting CLI",
		Long: `bitwave is the agent-friendly Bitwave CLI for plain-text accounting (PTA).

Every bitwave command operates on a workspace — a directory containing a
.bitwave.toml marker plus one or more .journal files. Run ` + "`bitwave init`" + ` in the
directory you want the workspace to live in BEFORE any other command. bitwave
walks up from the cwd to find the workspace marker, so you can run
commands from any subdirectory once it exists.

Modes:
  - Local (default): files live on disk. No auth needed. ` + "`bitwave share`" + ` also
    works anonymously in local mode (the recipient is the one who signs in
    to adopt the workspace).
  - Cloud (` + "`bitwave init --cloud`" + `): backed by gl-svc under your org. Requires
    ` + "`bitwave auth login`" + ` and ` + "`bitwave org use`" + `.

Auth (used by cloud-mode commands; priority order):
  - BITWAVE_AGENT_TOKEN env  Well-known agent identity
  - bitwave auth login           Human PKCE browser flow
  - bitwave auth delegate        Request delegated access from a user

Operating context is printed to stderr before every command as a one-line
banner: ` + "`bitwave: workspace=... | org=... | identity=...`" + `. Suppress with
--quiet or BITWAVE_QUIET=1. Run ` + "`bitwave status`" + ` to print it on demand.

Quickstart — log expenses and share a report:
  cd ~/my-expenses
  bitwave init                                                 # workspace in cwd
  bitwave expense new --report 2026-05 --date 2026-05-29 \
      --amount 10 --account Expenses:Meals --merchant Cafe
  bitwave expense new --report 2026-05 --date 2026-05-29 \
      --amount "1 ETH" --account Expenses:Crypto \
      --credit-account Assets:Crypto:ETH
  bitwave expense report 2026-05                               # render to stdout
  bitwave share --to me@example.com                            # email a link

Quickstart — raw double-entry journal:
  bitwave init
  bitwave acct add Assets:Cash
  bitwave acct add Income:Salary
  bitwave je new --date 2026-01-01 --payee "Initial" \
      --posting "Assets:Cash 1000 USD" \
      --posting "Income:Salary -1000 USD"
  bitwave bal

Tip: run ` + "`bitwave <command> --help`" + ` on any subcommand to see flags + examples.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(c *cobra.Command, _ []string) {
			printStatusBanner(c)
		},
		PersistentPostRun: func(c *cobra.Command, _ []string) {
			// Refresh the update-check cache at most once per 24h, after the
			// command's real work is done. Silent on failure; skipped for
			// quiet runs, dev builds, and BITWAVE_NO_UPDATE_CHECK=1.
			if !quietFlag && os.Getenv("BITWAVE_QUIET") != "1" {
				update.BackgroundRefresh(Version)
			}
		},
		Run: func(c *cobra.Command, _ []string) { _ = c.Help() },
	}

	const (
		groupAuth      = "auth"
		groupAccount   = "account"
		groupLedger    = "ledger"
		groupReports   = "reports"
		groupWorkflows = "workflows"
		groupCLI       = "cli"
	)
	root.AddGroup(
		&cobra.Group{ID: groupAuth, Title: "Authentication:"},
		&cobra.Group{ID: groupAccount, Title: "Org & workspace:"},
		&cobra.Group{ID: groupLedger, Title: "Ledger:"},
		&cobra.Group{ID: groupReports, Title: "Reports:"},
		&cobra.Group{ID: groupWorkflows, Title: "Workflows:"},
		&cobra.Group{ID: groupCLI, Title: "CLI:"},
	)

	addInGroup := func(group string, c *cobra.Command) {
		c.GroupID = group
		root.AddCommand(c)
	}

	root.PersistentFlags().StringVar(&authURLFlag, "auth-url", "", "")
	root.PersistentFlags().StringVar(&tokenFlag, "token", "", "Bitwave API token (env: BITWAVE_TOKEN)")
	root.PersistentFlags().BoolVar(&quietFlag, "quiet", false, "Suppress the bitwave status banner (also: BITWAVE_QUIET=1)")
	_ = root.PersistentFlags().MarkHidden("auth-url")

	addInGroup(groupAuth, newAuthCmd())
	addInGroup(groupAccount, newOrgCmd())
	addInGroup(groupAccount, newWorkspaceCmd())
	addInGroup(groupAccount, newJournalCmd())
	addInGroup(groupAccount, newInitCmd())

	addInGroup(groupLedger, newJECmd())
	addInGroup(groupLedger, newAcctCmd())
	addInGroup(groupLedger, newPriceCmd())
	addInGroup(groupLedger, newWalletsCmd())
	addInGroup(groupLedger, newExpenseCmd())

	addInGroup(groupReports, newBalCmd())
	addInGroup(groupReports, newRegCmd())
	addInGroup(groupReports, newPrintCmd())
	addInGroup(groupReports, newAccountsCmd())
	addInGroup(groupReports, newContactsCmd())
	addInGroup(groupReports, newCommoditiesCmd())
	addInGroup(groupReports, newEquityCmd())
	addInGroup(groupReports, newClearedCmd())
	addInGroup(groupReports, newCSVCmd())
	addInGroup(groupReports, newStatsCmd())

	addInGroup(groupWorkflows, newMigrateCmd())
	addInGroup(groupWorkflows, newCloseCmd())
	addInGroup(groupWorkflows, newShareCmd())
	addInGroup(groupWorkflows, newSharesCmd())

	addInGroup(groupCLI, newStatusCmd())
	addInGroup(groupCLI, newVersionCmd())
	addInGroup(groupCLI, newUpgradeCmd())

	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the bitwave CLI version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(auth.Banner)
			fmt.Printf("  bitwave v%s\n\n", Version)
		},
	}
}
