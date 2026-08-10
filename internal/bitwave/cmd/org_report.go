package cmd

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/apierr"
	"github.com/bitwave-io/bitwave-cli/internal/bitwave/config"
	"github.com/bitwave-io/bitwave-cli/internal/orgctx"
	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

type balanceReportFlags struct {
	asOf           string
	groupBy        string
	currency       string
	walletIDs      []string
	subsidiaryIDs  []string
	includeIgnored bool
	excludeNFT     bool
	skipPricing    bool
	format         string
	out            string
	orgID          string
	noWait         bool
	timeout        time.Duration
	reportAPI      string
}

func newOrgReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Run reports against Bitwave organization product data",
		Long: `Run reports against wallets and transactions in a Bitwave organization.

This command family is separate from ledger workspace reports such as
` + "`bitwave bal`" + `. Organization reports require authentication and an active
organization, but do not require a .bitwave.toml workspace.`,
	}
	cmd.AddCommand(newOrgReportListCmd())
	cmd.AddCommand(newOrgBalanceReportCmd())
	return cmd
}

func newOrgReportListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List organization reports supported by this CLI",
		Run: func(cmd *cobra.Command, _ []string) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "balance  Balance Report (wallet or asset grouping; CSV download)")
		},
	}
}

func newOrgBalanceReportCmd() *cobra.Command {
	var f balanceReportFlags
	cmd := &cobra.Command{
		Use:   "balance",
		Short: "Run and download an organization Balance Report",
		Long: `Runs Bitwave's server-side Balance Report against the selected organization's
product wallets and transactions. This is not the CLI ledger command ` + "`bitwave bal`" + `.

When --out is omitted the CSV is written to stdout. Status and provenance are
written to stderr so redirected CSV remains clean.`,
		Example: `  bitwave report balance --as-of 2026-06-30 --group-by wallet --out balance.csv
  bitwave report balance --as-of 2026-06-30 --group-by asset > balance.csv
  bitwave report balance --as-of 2026-06-30 --no-wait`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOrgBalanceReport(cmd, f)
		},
	}
	cmd.Flags().StringVar(&f.asOf, "as-of", "", "Balance date in YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&f.groupBy, "group-by", "wallet", "Grouping: wallet or asset")
	cmd.Flags().StringVar(&f.currency, "currency", "", "Fiat currency code (defaults to org base currency)")
	cmd.Flags().StringSliceVar(&f.walletIDs, "wallet", nil, "Wallet ID filter (repeatable or comma-separated)")
	cmd.Flags().StringSliceVar(&f.subsidiaryIDs, "subsidiary", nil, "Subsidiary ID filter (repeatable or comma-separated)")
	cmd.Flags().BoolVar(&f.includeIgnored, "include-ignored", false, "Include ignored transactions")
	cmd.Flags().BoolVar(&f.excludeNFT, "exclude-nft", false, "Exclude NFT balances")
	cmd.Flags().BoolVar(&f.skipPricing, "skip-pricing", false, "Do not calculate fiat values")
	cmd.Flags().StringVar(&f.format, "format", "csv", "Output format (csv)")
	cmd.Flags().StringVarP(&f.out, "out", "o", "", "Output file (stdout when omitted)")
	cmd.Flags().StringVar(&f.orgID, "org", "", "Organization ID override")
	cmd.Flags().BoolVar(&f.noWait, "no-wait", false, "Start the report, print its run ID, and exit")
	cmd.Flags().DurationVar(&f.timeout, "timeout", 15*time.Minute, "Maximum time to wait for report completion")
	cmd.Flags().StringVar(&f.reportAPI, "report-api", "v1", "Report API: v1 (production-stable) or v3 (preview)")
	_ = cmd.MarkFlagRequired("as-of")
	return cmd
}

func runOrgBalanceReport(cmd *cobra.Command, f balanceReportFlags) error {
	if err := validateBalanceReportFlags(f); err != nil {
		return err
	}
	orgID, err := resolveReportOrg(f.orgID)
	if err != nil {
		return err
	}

	inputs, err := balanceReportInputs(f)
	if err != nil {
		return err
	}
	client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
	if f.reportAPI == "v1" {
		return runLegacyOrgBalanceReport(cmd, client, orgID, f)
	}
	run, err := client.StartBalance(cmd.Context(), orgID, inputs)
	if err != nil {
		return fmt.Errorf("start organization balance report: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "source=bitwave-org-report org=%s report=balance asOf=%s groupBy=%s runId=%s\n", orgID, f.asOf, f.groupBy, run.ReportRunID)

	if f.noWait {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), run.ReportRunID)
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.timeout)
	defer cancel()
	if err := waitForReport(ctx, client, orgID, run.ReportRunID); err != nil {
		return err
	}

	csv, usedFallback, err := downloadBalanceCSV(ctx, client, orgID, run.ReportRunID)
	if err != nil {
		return fmt.Errorf("download organization balance report %s: %w", run.ReportRunID, err)
	}
	if usedFallback {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "download-route=unavailable fallback=report-result")
	}
	if f.out == "" || f.out == "-" {
		_, err = cmd.OutOrStdout().Write(csv)
		return err
	}
	if err := writeFileAtomic(f.out, csv); err != nil {
		return fmt.Errorf("save balance report: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "saved=%s bytes=%d\n", f.out, len(csv))
	return nil
}

func runLegacyOrgBalanceReport(cmd *cobra.Command, client *orgreports.Client, orgID string, f balanceReportFlags) error {
	if f.currency != "" || len(f.walletIDs) > 0 || f.includeIgnored || f.excludeNFT || f.skipPricing {
		return errors.New("--currency, --wallet, --include-ignored, --exclude-nft, and --skip-pricing require --report-api v3; production V3 result/download routes are not currently available")
	}
	run, err := client.StartLegacyBalance(cmd.Context(), orgID, orgreports.LegacyBalanceInput{
		EndDate:       f.asOf,
		GroupBy:       f.groupBy,
		SubsidiaryIDs: f.subsidiaryIDs,
	})
	if err != nil {
		return fmt.Errorf("start organization balance report: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "source=bitwave-org-report api=v1 org=%s report=balance asOf=%s groupBy=%s runId=%s\n", orgID, f.asOf, f.groupBy, run.ID)
	if f.noWait {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), run.ID)
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.timeout)
	defer cancel()
	report, err := waitForLegacyReport(ctx, client, orgID, run.ID)
	if err != nil {
		return err
	}
	results, ok := report.Links["results"]
	if !ok || results.Href == "" {
		return fmt.Errorf("organization balance report %s succeeded but returned no results download link", run.ID)
	}
	data, err := client.DownloadLink(ctx, results.Href)
	if err != nil {
		return fmt.Errorf("download organization balance report %s: %w", run.ID, err)
	}
	if f.out == "" || f.out == "-" {
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}
	if err := writeFileAtomic(f.out, data); err != nil {
		return fmt.Errorf("save balance report: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "saved=%s bytes=%d\n", f.out, len(data))
	return nil
}

func waitForLegacyReport(ctx context.Context, client *orgreports.Client, orgID, runID string) (*orgreports.LegacyReport, error) {
	delay := 2 * time.Second
	for {
		report, err := client.LegacyReport(ctx, orgID, runID, true)
		if err != nil {
			return nil, fmt.Errorf("check organization balance report %s: %w", runID, err)
		}
		switch report.Data.Status {
		case "succeeded":
			return report, nil
		case "failed", "timed-out":
			return nil, fmt.Errorf("organization balance report %s ended with status %s", runID, report.Data.Status)
		case "new", "running":
		default:
			return nil, fmt.Errorf("organization balance report %s returned unknown status %q", runID, report.Data.Status)
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("waiting for organization balance report %s: %w", runID, ctx.Err())
		case <-timer.C:
		}
		if delay < 10*time.Second {
			delay *= 2
			if delay > 10*time.Second {
				delay = 10 * time.Second
			}
		}
	}
}

type reportDownloadClient interface {
	Download(context.Context, string, string) ([]byte, error)
	Result(context.Context, string, string) (*orgreports.ReportData, error)
}

func downloadBalanceCSV(ctx context.Context, client reportDownloadClient, orgID, runID string) ([]byte, bool, error) {
	data, err := client.Download(ctx, orgID, runID)
	if err == nil {
		return data, false, nil
	}
	var apiError *apierr.Error
	if !errors.As(err, &apiError) || apiError.Status != 404 {
		return nil, false, err
	}

	result, resultErr := client.Result(ctx, orgID, runID)
	if resultErr != nil {
		return nil, true, fmt.Errorf("CSV endpoint unavailable (%v); report-result fallback failed: %w", err, resultErr)
	}
	data, resultErr = reportDataCSV(result)
	return data, true, resultErr
}

func reportDataCSV(result *orgreports.ReportData) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(result.Columns); err != nil {
		return nil, err
	}
	var writeRows func([]orgreports.ReportRow) error
	writeRows = func(rows []orgreports.ReportRow) error {
		for _, row := range rows {
			cells := append([]string(nil), row.Cells...)
			if len(cells) < len(result.Columns) {
				cells = append(cells, make([]string, len(result.Columns)-len(cells))...)
			}
			if len(cells) > len(result.Columns) {
				return fmt.Errorf("report row has %d cells but report defines %d columns", len(cells), len(result.Columns))
			}
			if err := w.Write(cells); err != nil {
				return err
			}
			if err := writeRows(row.Rows); err != nil {
				return err
			}
		}
		return nil
	}
	if err := writeRows(result.Rows); err != nil {
		return nil, err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type reportStatusClient interface {
	Status(context.Context, string, string) (*orgreports.RunStatus, error)
}

func waitForReport(ctx context.Context, client reportStatusClient, orgID, runID string) error {
	delay := 2 * time.Second
	for {
		status, err := client.Status(ctx, orgID, runID)
		if err != nil {
			return fmt.Errorf("check organization balance report %s: %w", runID, err)
		}
		switch status.Status {
		case "succeeded":
			return nil
		case "failed", "timed-out":
			return fmt.Errorf("organization balance report %s ended with status %s", runID, status.Status)
		case "new", "running":
			// Continue below.
		default:
			return fmt.Errorf("organization balance report %s returned unknown status %q", runID, status.Status)
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("waiting for organization balance report %s: %w", runID, ctx.Err())
		case <-timer.C:
		}
		if delay < 10*time.Second {
			delay *= 2
			if delay > 10*time.Second {
				delay = 10 * time.Second
			}
		}
	}
}

func validateBalanceReportFlags(f balanceReportFlags) error {
	parsed, err := time.Parse("2006-01-02", f.asOf)
	if err != nil || parsed.Format("2006-01-02") != f.asOf {
		return fmt.Errorf("--as-of must be a valid calendar date in YYYY-MM-DD format")
	}
	if f.groupBy != "wallet" && f.groupBy != "asset" {
		return fmt.Errorf("--group-by must be wallet or asset")
	}
	if strings.ToLower(f.format) != "csv" {
		return fmt.Errorf("--format currently supports csv only")
	}
	if f.timeout <= 0 {
		return fmt.Errorf("--timeout must be greater than zero")
	}
	if f.reportAPI != "v1" && f.reportAPI != "v3" {
		return fmt.Errorf("--report-api must be v1 or v3")
	}
	return nil
}

func balanceReportInputs(f balanceReportFlags) ([]orgreports.Input, error) {
	groupCode := "1"
	if f.groupBy == "asset" {
		groupCode = "0"
	}
	inputs := []orgreports.Input{
		{Key: "endDate", Value: f.asOf},
		{Key: "groupBy", Value: groupCode},
	}
	appendString := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			inputs = append(inputs, orgreports.Input{Key: key, Value: strings.TrimSpace(value)})
		}
	}
	appendBool := func(key string, value bool) {
		if value {
			inputs = append(inputs, orgreports.Input{Key: key, Value: "true"})
		}
	}
	appendArray := func(key string, values []string) error {
		if len(values) == 0 {
			return nil
		}
		data, err := json.Marshal(values)
		if err != nil {
			return err
		}
		inputs = append(inputs, orgreports.Input{Key: key, Value: string(data)})
		return nil
	}
	appendString("currency", strings.ToUpper(f.currency))
	if err := appendArray("walletIds", f.walletIDs); err != nil {
		return nil, err
	}
	if err := appendArray("subsidiaryIds", f.subsidiaryIDs); err != nil {
		return nil, err
	}
	appendBool("includeIgnored", f.includeIgnored)
	appendBool("excludeNft", f.excludeNFT)
	appendBool("skipPricing", f.skipPricing)
	return inputs, nil
}

func resolveReportOrg(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	var workspaceOrg string
	if cwd, err := os.Getwd(); err == nil {
		if dir, err := config.Find(cwd); err == nil {
			if cfg, err := config.Load(dir); err == nil && cfg.Mode == config.ModeCloud {
				workspaceOrg = cfg.OrgId
			}
		}
	}
	if workspaceOrg != "" {
		if active, err := orgctx.Load(); err == nil && active.OrgID != "" && active.OrgID != workspaceOrg {
			return "", fmt.Errorf("workspace org %s differs from active org %s; pass --org explicitly", workspaceOrg, active.OrgID)
		}
		return workspaceOrg, nil
	}
	if envOrg := os.Getenv("BITWAVE_ORG_ID"); envOrg != "" {
		return envOrg, nil
	}
	active, err := orgctx.Load()
	if err != nil || active.OrgID == "" {
		return "", errors.New("no organization selected; run `bitwave org use ORG_ID` or pass `--org ORG_ID`")
	}
	return active.OrgID, nil
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, "."+base+"-*.partial")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
