package cmd

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

type balanceComparisonRow struct {
	Token        string `json:"token"`
	DashboardQty string `json:"dashboardQty,omitempty"`
	ReportQty    string `json:"reportQty,omitempty"`
	Difference   string `json:"difference"`
	Matches      bool   `json:"matches"`
}

func newOrgBalanceCompareCmd() *cobra.Command {
	var orgID, asOf, viewRef, currency, toleranceText, out string
	var mismatchesOnly bool
	cmd := &cobra.Command{
		Use:     "balance-compare",
		Aliases: []string{"compare-balance"},
		Short:   "Compare Balance Report quantities with an inventory dashboard",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateExportDateRange(asOf, asOf, false); err != nil {
				return errors.New("--as-of must be a valid calendar date in YYYY-MM-DD format")
			}
			if strings.TrimSpace(viewRef) == "" {
				return errors.New("--inventory-view is required")
			}
			tolerance, ok := new(big.Rat).SetString(strings.TrimSpace(toleranceText))
			if !ok || tolerance.Sign() < 0 {
				return errors.New("--tolerance must be a non-negative decimal")
			}
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			views, err := client.InventoryViews(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("list inventory views: %w", err)
			}
			view, err := resolveInventoryView(viewRef, views)
			if err != nil {
				return err
			}
			organization, err := client.Org(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("load organization settings: %w", err)
			}
			if strings.TrimSpace(currency) == "" {
				currency = organizationCurrency(organization.BaseCurrency)
			}
			currency = strings.ToUpper(strings.TrimSpace(currency))
			if currency == "" {
				return errors.New("could not resolve the organization currency; pass --currency")
			}

			var dashboardRaw, reportRaw json.RawMessage
			var dashboardErr, reportErr error
			var requests sync.WaitGroup
			requests.Add(2)
			go func() {
				defer requests.Done()
				dashboardRaw, dashboardErr = client.DashboardBalance(cmd.Context(), resolvedOrg, view.ID, asOf)
			}()
			go func() {
				defer requests.Done()
				reportRaw, reportErr = client.AssetBalanceReport(cmd.Context(), resolvedOrg, asOf, currency)
			}()
			requests.Wait()
			if dashboardErr != nil {
				return fmt.Errorf("load dashboard balances: %w", dashboardErr)
			}
			if reportErr != nil {
				return fmt.Errorf("run balance report: %w", reportErr)
			}
			dashboard, err := quantitiesFromDashboard(dashboardRaw)
			if err != nil {
				return err
			}
			report, err := quantitiesFromReport(reportRaw)
			if err != nil {
				return err
			}
			rows := compareQuantities(dashboard, report, tolerance, mismatchesOnly)
			mismatches := 0
			for _, row := range rows {
				if !row.Matches {
					mismatches++
				}
			}
			if out != "" && out != "-" {
				data, csvErr := balanceComparisonCSV(rows)
				if csvErr != nil {
					return csvErr
				}
				if err := writeFileAtomic(out, data); err != nil {
					return fmt.Errorf("save balance comparison: %w", err)
				}
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": "1", "organization": resolvedOrg, "asOf": asOf, "currency": currency,
				"inventoryView": view, "tolerance": toleranceText, "dashboardAssetCount": len(dashboard),
				"reportAssetCount": len(report), "mismatchCount": mismatches, "output": out, "rows": rows,
			})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().StringVar(&asOf, "as-of", "", "Balance date in YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&viewRef, "inventory-view", "", "Inventory view ID or exact name (required)")
	cmd.Flags().StringVar(&currency, "currency", "", "Report currency (defaults to organization base currency)")
	cmd.Flags().StringVar(&toleranceText, "tolerance", "0", "Maximum absolute quantity difference considered a match")
	cmd.Flags().BoolVar(&mismatchesOnly, "mismatches-only", false, "Return only nonmatching assets")
	cmd.Flags().StringVarP(&out, "out", "o", "", "Optional CSV output path")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func organizationCurrency(value any) string {
	if object, ok := value.(map[string]any); ok {
		return firstString(object["ticker"], object["code"], object["symbol"])
	}
	text := firstString(value)
	if code := map[string]string{
		"1": "USD", "2": "GBP", "3": "JPY", "4": "EUR", "5": "SGD", "6": "CHF", "7": "CAD", "8": "KRW",
		"9": "NOK", "10": "DKK", "11": "NZD", "12": "ISK", "13": "HKD", "14": "CNY", "15": "PLN", "16": "AUD",
	}[text]; code != "" {
		return code
	}
	if len(text) >= 3 && len(text) <= 5 {
		return text
	}
	return ""
}

func quantitiesFromDashboard(raw json.RawMessage) (map[string]*big.Rat, error) {
	object, err := rawJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("decode dashboard balance: %w", err)
	}
	lines, _ := object["lines"].(map[string]any)
	items, _ := lines["items"].([]any)
	return quantitiesFromItems(items, "asset", "qty"), nil
}

func quantitiesFromReport(raw json.RawMessage) (map[string]*big.Rat, error) {
	object, err := rawJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("decode balance report: %w", err)
	}
	items, _ := object["lines"].([]any)
	return quantitiesFromItems(items, "ticker", "value"), nil
}

func rawJSONObject(raw json.RawMessage) (map[string]any, error) {
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err == nil {
		return object, nil
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, err
	}
	decoder = json.NewDecoder(strings.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	return object, nil
}

func quantitiesFromItems(items []any, keyField, valueField string) map[string]*big.Rat {
	result := map[string]*big.Rat{}
	for _, item := range items {
		row, _ := item.(map[string]any)
		key := strings.TrimSpace(firstString(row[keyField]))
		valueText := strings.ReplaceAll(firstString(row[valueField]), ",", "")
		value, ok := new(big.Rat).SetString(valueText)
		if key == "" || !ok {
			continue
		}
		if existing := result[key]; existing != nil {
			existing.Add(existing, value)
		} else {
			result[key] = value
		}
	}
	return result
}

func compareQuantities(dashboard, report map[string]*big.Rat, tolerance *big.Rat, mismatchesOnly bool) []balanceComparisonRow {
	keys := make([]string, 0, len(dashboard)+len(report))
	seen := map[string]bool{}
	for key := range dashboard {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range report {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	rows := make([]balanceComparisonRow, 0, len(keys))
	for _, key := range keys {
		dashboardQty := new(big.Rat)
		reportQty := new(big.Rat)
		if value := dashboard[key]; value != nil {
			dashboardQty.Set(value)
		}
		if value := report[key]; value != nil {
			reportQty.Set(value)
		}
		difference := new(big.Rat).Sub(reportQty, dashboardQty)
		absolute := new(big.Rat).Abs(new(big.Rat).Set(difference))
		matches := absolute.Cmp(tolerance) <= 0
		if mismatchesOnly && matches {
			continue
		}
		rows = append(rows, balanceComparisonRow{Token: key, DashboardQty: decimalString(dashboardQty), ReportQty: decimalString(reportQty), Difference: decimalString(difference), Matches: matches})
	}
	return rows
}

func balanceComparisonCSV(rows []balanceComparisonRow) ([]byte, error) {
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{"token", "dashboard_qty", "report_qty", "difference", "matches"}); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := writer.Write([]string{row.Token, row.DashboardQty, row.ReportQty, row.Difference, fmt.Sprint(row.Matches)}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return output.Bytes(), writer.Error()
}
