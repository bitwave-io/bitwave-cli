package cmd

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-accounting-sdk/model"
	"github.com/bitwave-io/bitwave-accounting-sdk/report"
)

// loadProject is the report-side loader, parallel to bw's loadProject.
func loadProject(ctx context.Context) (*model.Project, error) {
	s, _, _, err := resolveStore(ctx)
	if err != nil {
		return nil, err
	}
	return s.Project(ctx)
}

func buildFilter(from, to, account string, clearedOnly bool) report.Filter {
	f := report.Filter{AccountMatch: account, ClearedOnly: clearedOnly}
	if from != "" {
		if t, err := time.Parse("2006-01-02", from); err == nil {
			f.From = t
		}
	}
	if to != "" {
		if t, err := time.Parse("2006-01-02", to); err == nil {
			f.To = t
		}
	}
	return f
}

func addReportFilters(c *cobra.Command, from, to, account *string, clearedOnly *bool) {
	c.Flags().StringVar(from, "from", "", "Earliest date (YYYY-MM-DD)")
	c.Flags().StringVar(to, "to", "", "Latest date (YYYY-MM-DD)")
	c.Flags().StringVar(account, "account", "", "Account name substring filter")
	if clearedOnly != nil {
		c.Flags().BoolVar(clearedOnly, "cleared", false, "Cleared entries only")
	}
}

func newPrintCmd() *cobra.Command {
	var from, to, account string
	var cleared bool
	cmd := &cobra.Command{
		Use:   "print",
		Short: "Re-emit canonical ledger format",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := loadProject(cmd.Context())
			if err != nil {
				return err
			}
			return report.Print(os.Stdout, p, buildFilter(from, to, account, cleared))
		},
	}
	addReportFilters(cmd, &from, &to, &account, &cleared)
	return cmd
}

func newBalCmd() *cobra.Command {
	var from, to, account string
	var cleared bool
	cmd := &cobra.Command{
		Use:     "bal [account-substring]",
		Aliases: []string{"balance"},
		Short:   "Account balances tree",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := loadProject(cmd.Context())
			if err != nil {
				return err
			}
			if len(args) == 1 && account == "" {
				account = args[0]
			}
			return report.Balance(os.Stdout, p, buildFilter(from, to, account, cleared))
		},
	}
	addReportFilters(cmd, &from, &to, &account, &cleared)
	return cmd
}

func newRegCmd() *cobra.Command {
	var from, to, account string
	var cleared bool
	cmd := &cobra.Command{
		Use:     "reg [account-substring]",
		Aliases: []string{"register"},
		Short:   "Posting register with running balance",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := loadProject(cmd.Context())
			if err != nil {
				return err
			}
			if len(args) == 1 && account == "" {
				account = args[0]
			}
			return report.Register(os.Stdout, p, buildFilter(from, to, account, cleared))
		},
	}
	addReportFilters(cmd, &from, &to, &account, &cleared)
	return cmd
}

func newAccountsCmd() *cobra.Command {
	var account string
	cmd := &cobra.Command{
		Use:   "accounts",
		Short: "List declared and observed accounts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := loadProject(cmd.Context())
			if err != nil {
				return err
			}
			return report.Accounts(os.Stdout, p, report.Filter{AccountMatch: account})
		},
	}
	cmd.Flags().StringVar(&account, "account", "", "Account name substring filter")
	return cmd
}

// newContactsCmd: ledger-cli's "payees" report — renamed because gl-svc uses
// the directionally-neutral "contacts" terminology (matching Xero/QuickBooks).
func newContactsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "contacts",
		Aliases: []string{"payees"},
		Short:   "Distinct contacts (payees + payors) referenced by entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := loadProject(cmd.Context())
			if err != nil {
				return err
			}
			return report.Payees(os.Stdout, p)
		},
	}
	return cmd
}

func newCommoditiesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "commodities",
		Short: "Distinct commodities (asset symbols)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := loadProject(cmd.Context())
			if err != nil {
				return err
			}
			return report.Commodities(os.Stdout, p)
		},
	}
}

func newEquityCmd() *cobra.Command {
	var from, to, account string
	cmd := &cobra.Command{
		Use:   "equity",
		Short: "Equity-style snapshot entry",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := loadProject(cmd.Context())
			if err != nil {
				return err
			}
			return report.Equity(os.Stdout, p, buildFilter(from, to, account, false))
		},
	}
	addReportFilters(cmd, &from, &to, &account, nil)
	return cmd
}

func newClearedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cleared",
		Short: "Print only cleared entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := loadProject(cmd.Context())
			if err != nil {
				return err
			}
			return report.Cleared(os.Stdout, p)
		},
	}
}

func newCSVCmd() *cobra.Command {
	var from, to, account string
	var cleared bool
	cmd := &cobra.Command{
		Use:   "csv",
		Short: "CSV dump of postings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := loadProject(cmd.Context())
			if err != nil {
				return err
			}
			return report.CSVPrint(os.Stdout, p, buildFilter(from, to, account, cleared))
		},
	}
	addReportFilters(cmd, &from, &to, &account, &cleared)
	return cmd
}

func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Workspace summary counts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := loadProject(cmd.Context())
			if err != nil {
				return err
			}
			return report.Stats(os.Stdout, p)
		},
	}
}
