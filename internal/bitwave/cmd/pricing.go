package cmd

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

type historicPriceRow struct {
	Coin         string          `json:"coin"`
	TimestampSEC int64           `json:"timestampSEC"`
	Timestamp    string          `json:"timestamp"`
	Status       string          `json:"status,omitempty"`
	Price        json.RawMessage `json:"price,omitempty"`
	Steps        json.RawMessage `json:"steps,omitempty"`
}

func newPricingCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "pricing", Short: "Analyze organization pricing history"}
	cmd.AddCommand(newPricingHistoryCmd())
	return cmd
}

func newPricingHistoryCmd() *cobra.Command {
	var orgID, from, to, out string
	var coins []string
	var pageSize, limitPerCoin int
	var excludeSpam bool
	var spamThreshold float64
	cmd := &cobra.Command{
		Use: "history", Aliases: []string{"report"}, Short: "Generate a historic pricing report for selected tokens", Args: cobra.NoArgs,
		Long: `Fetch Bitwave's historic price provenance for one or more organization
tokens. Date ranges are interpreted in the organization's configured timezone
and are capped at 31 days. By default JSON is written to stdout; --out writes
a flattened CSV and leaves a compact result envelope on stdout.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			coins = uniqueNonEmpty(coins)
			if len(coins) == 0 {
				return errors.New("at least one --coin is required")
			}
			if pageSize < 1 || pageSize > 500 {
				return errors.New("--page-size must be between 1 and 500")
			}
			if limitPerCoin < 1 || limitPerCoin > 10000 {
				return errors.New("--limit-per-coin must be between 1 and 10000")
			}
			if spamThreshold < 0 || spamThreshold > 1 {
				return errors.New("--spam-threshold must be between 0 and 1")
			}
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			organization, err := client.Org(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("load organization settings: %w", err)
			}
			fromSEC, toSEC, err := pricingDateRange(from, to, organization.Timezone)
			if err != nil {
				return err
			}
			skippedSpam := make([]string, 0)
			rows := make([]historicPriceRow, 0)
			for _, coin := range coins {
				if excludeSpam {
					metadata, lookupErr := client.PublicSymbol(cmd.Context(), coin)
					if lookupErr == nil && len(spamRows(coin, metadata, spamThreshold)) > 0 {
						skippedSpam = append(skippedSpam, coin)
						continue
					}
				}
				coinRows, fetchErr := collectHistoricPrices(cmd, client, resolvedOrg, coin, fromSEC, toSEC, pageSize, limitPerCoin)
				if fetchErr != nil {
					return fmt.Errorf("historic prices for %s: %w", coin, fetchErr)
				}
				rows = append(rows, coinRows...)
			}
			sort.SliceStable(rows, func(i, j int) bool {
				if rows[i].Coin != rows[j].Coin {
					return rows[i].Coin < rows[j].Coin
				}
				return rows[i].TimestampSEC < rows[j].TimestampSEC
			})
			if out != "" && out != "-" {
				data, csvErr := historicPricesCSV(rows)
				if csvErr != nil {
					return csvErr
				}
				if err := writeFileAtomic(out, data); err != nil {
					return fmt.Errorf("save pricing report: %w", err)
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "rowCount": len(rows), "output": out, "skippedSpam": skippedSpam})
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "timezone": organization.Timezone, "from": from, "to": to, "skippedSpam": skippedSpam, "rows": rows})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().StringSliceVar(&coins, "coin", nil, "Token symbol (repeatable or comma-separated; required)")
	cmd.Flags().StringVar(&from, "from", "", "Inclusive start date in YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&to, "to", "", "Inclusive end date in YYYY-MM-DD (required; maximum 31 days)")
	cmd.Flags().IntVar(&pageSize, "page-size", 100, "Historic-price API page size")
	cmd.Flags().IntVar(&limitPerCoin, "limit-per-coin", 1000, "Maximum price records per token")
	cmd.Flags().BoolVar(&excludeSpam, "exclude-spam", false, "Skip tokens flagged by metadata spam scoring")
	cmd.Flags().Float64Var(&spamThreshold, "spam-threshold", 0.5, "Spam score used with --exclude-spam")
	cmd.Flags().StringVarP(&out, "out", "o", "", "Optional CSV output path")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON")
	return cmd
}

func pricingDateRange(from, to, timezone string) (int64, int64, error) {
	if err := validateExportDateRange(from, to, false); err != nil {
		return 0, 0, err
	}
	fromDate, _ := time.Parse("2006-01-02", from)
	toDate, _ := time.Parse("2006-01-02", to)
	if toDate.Sub(fromDate) > 30*24*time.Hour {
		return 0, 0, errors.New("pricing history date range cannot exceed 31 calendar days")
	}
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid organization timezone %q: %w", timezone, err)
	}
	start := time.Date(fromDate.Year(), fromDate.Month(), fromDate.Day(), 0, 0, 0, 0, location)
	end := time.Date(toDate.Year(), toDate.Month(), toDate.Day()+1, 0, 0, 0, 0, location).Add(-time.Second)
	return start.Unix(), end.Unix(), nil
}

func collectHistoricPrices(cmd *cobra.Command, client *orgreports.Client, orgID, coin string, fromSEC, toSEC int64, pageSize, limit int) ([]historicPriceRow, error) {
	rows := make([]historicPriceRow, 0)
	pageToken := ""
	seenTokens := map[string]bool{}
	for {
		page, err := client.HistoricPrices(cmd.Context(), orgID, coin, fromSEC, toSEC, min(pageSize, limit-len(rows)), pageToken)
		if err != nil {
			return nil, err
		}
		for _, price := range page.Prices {
			if price.TimestampSEC < fromSEC || price.TimestampSEC > toSEC {
				continue
			}
			rows = append(rows, historicPriceRow{Coin: coin, TimestampSEC: price.TimestampSEC, Timestamp: time.Unix(price.TimestampSEC, 0).UTC().Format(time.RFC3339), Status: price.Status, Price: price.Price, Steps: price.Steps})
			if len(rows) >= limit {
				return rows, nil
			}
		}
		if !page.HasMore || page.NextPageToken == "" || seenTokens[page.NextPageToken] {
			return rows, nil
		}
		seenTokens[page.NextPageToken] = true
		pageToken = page.NextPageToken
	}
}

func historicPricesCSV(rows []historicPriceRow) ([]byte, error) {
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{"coin", "timestampSEC", "timestampUTC", "status", "priceType", "price", "open", "close", "high", "low", "volume", "steps"}); err != nil {
		return nil, err
	}
	for _, row := range rows {
		var price map[string]any
		decoder := json.NewDecoder(bytes.NewReader(row.Price))
		decoder.UseNumber()
		_ = decoder.Decode(&price)
		if err := writer.Write([]string{
			row.Coin, fmt.Sprint(row.TimestampSEC), row.Timestamp, row.Status,
			firstString(price["type"]), firstString(price["price"]), firstString(price["open"]),
			firstString(price["close"]), firstString(price["high"]), firstString(price["low"]),
			firstString(price["volume"]), strings.TrimSpace(string(row.Steps)),
		}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return output.Bytes(), writer.Error()
}
