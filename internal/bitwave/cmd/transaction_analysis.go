package cmd

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func newTransactionCountCmd() *cobra.Command {
	var orgID, from, to string
	var walletRefs, assets, ignored []string
	cmd := &cobra.Command{
		Use: "count", Short: "Count transactions and categorization/reconciliation states", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			from, to, err := defaultAnalysisDates(from, to)
			if err != nil {
				return err
			}
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			walletIDs := uniqueNonEmpty(walletRefs)
			if len(walletIDs) > 0 {
				wallets, discoverErr := client.Wallets(cmd.Context(), resolvedOrg)
				if discoverErr != nil {
					return fmt.Errorf("resolve wallets: %w", discoverErr)
				}
				walletIDs, err = resolveWalletRefs(walletIDs, wallets)
				if err != nil {
					return err
				}
			}
			filters := orgreports.TransactionCountFilters{WalletIDs: walletIDs, IgnoredStatuses: uniqueNonEmpty(ignored), AmountCurrencyNames: uniqueNonEmpty(assets)}
			filters.DateRange.From, filters.DateRange.To = from, to
			count, err := client.TransactionCount(cmd.Context(), resolvedOrg, filters)
			if err != nil {
				return fmt.Errorf("count transactions: %w", err)
			}
			categorized := count.All - count.NeedsCategorization
			reconciled := count.All - count.ToBeReconciled
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": "1", "organization": resolvedOrg, "filters": filters,
				"counts":          map[string]any{"all": count.All, "uncategorized": count.NeedsCategorization, "categorized": categorized, "unreconciled": count.ToBeReconciled, "reconciled": reconciled},
				"firstRecordDate": count.FirstRecordDate,
			})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().StringVar(&from, "from", "", "Inclusive start date (defaults to 2000-01-01)")
	cmd.Flags().StringVar(&to, "to", "", "Inclusive end date (defaults to today)")
	cmd.Flags().StringSliceVar(&walletRefs, "wallet", nil, "Wallet ID or exact name (repeatable)")
	cmd.Flags().StringSliceVar(&assets, "asset", nil, "Token/currency name (repeatable)")
	cmd.Flags().StringSliceVar(&ignored, "ignored", []string{"Unignored"}, "Ignored-status filter")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

type negativeLine struct {
	Timestamp time.Time
	TimeText  string
	Operation string
	Amount    *big.Rat
	Balance   *big.Rat
	ID        string
	LineID    string
}

type negativeOutputRow struct {
	Timestamp string `json:"timestamp"`
	Operation string `json:"operation"`
	Amount    string `json:"amount"`
	Balance   string `json:"balance"`
	ID        string `json:"id"`
	LineID    string `json:"lineId,omitempty"`
}

func newTransactionNegativesCmd() *cobra.Command {
	var orgID, from, to, out string
	var maxTransactions int
	cmd := &cobra.Command{
		Use: "negatives WALLET_ID_OR_NAME TOKEN", Short: "Build a running token balance and locate its first negative point", Args: cobra.ExactArgs(2),
		Long: `Read unignored transactions for one wallet in ascending order, normalize
inflow/outflow signs, and calculate an exact decimal running balance for the
selected token. Use --out to also save every contributing line as CSV.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if maxTransactions < 1 || maxTransactions > 1000000 {
				return errors.New("--max-transactions must be between 1 and 1000000")
			}
			from, to, err := defaultAnalysisDates(from, to)
			if err != nil {
				return err
			}
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			org, err := client.Org(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("load organization settings: %w", err)
			}
			wallets, err := client.Wallets(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("resolve wallet: %w", err)
			}
			walletIDs, err := resolveWalletRefs([]string{args[0]}, wallets)
			if err != nil {
				return err
			}
			walletID, token := walletIDs[0], strings.TrimSpace(args[1])
			if token == "" {
				return errors.New("TOKEN is required")
			}
			lines, transactionCount, pageCount, truncated, err := collectNegativeLines(cmd, client, resolvedOrg, org.Timezone, walletID, token, from, to, maxTransactions)
			if err != nil {
				return err
			}
			rows, firstNegative, lowest := calculateNegativeBalances(lines)
			if out != "" && out != "-" {
				data, csvErr := negativeRowsCSV(rows)
				if csvErr != nil {
					return csvErr
				}
				if err := writeFileAtomic(out, data); err != nil {
					return fmt.Errorf("save negative-balance report: %w", err)
				}
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": "1", "organization": resolvedOrg, "walletId": walletID, "token": token,
				"dateRange": map[string]string{"from": from, "to": to}, "transactionsRead": transactionCount,
				"pagesRead": pageCount, "contributingLines": len(rows), "firstNegative": firstNegative,
				"lowestBalance": lowest, "truncated": truncated, "maxTransactions": maxTransactions,
				"output": out, "rows": rows,
			})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().StringVar(&from, "from", "", "Inclusive start date (defaults to 2000-01-01)")
	cmd.Flags().StringVar(&to, "to", "", "Inclusive end date (defaults to today)")
	cmd.Flags().IntVar(&maxTransactions, "max-transactions", 10000, "Safety cap on transactions read")
	cmd.Flags().StringVarP(&out, "out", "o", "", "Optional CSV output path")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func defaultAnalysisDates(from, to string) (string, string, error) {
	if strings.TrimSpace(from) == "" {
		from = "2000-01-01"
	}
	if strings.TrimSpace(to) == "" {
		to = time.Now().UTC().Format("2006-01-02")
	}
	if err := validateExportDateRange(from, to, false); err != nil {
		return "", "", err
	}
	return from, to, nil
}

func collectNegativeLines(cmd *cobra.Command, client *orgreports.Client, orgID, timezone, walletID, token, from, to string, maxTransactions int) ([]negativeLine, int, int, bool, error) {
	result := make([]negativeLine, 0)
	seenLines := map[string]bool{}
	nextToken := ""
	transactionsRead, pages := 0, 0
	for {
		pageLimit := min(100, maxTransactions-transactionsRead)
		if pageLimit <= 0 {
			return result, transactionsRead, pages, true, nil
		}
		request := orgreports.TransactionSearchRequest{
			Timezone: timezone, Limit: pageLimit, NextToken: nextToken, SortBy: "timestamp", SortDirection: "asc",
			Filters: orgreports.TransactionExportFilters{DateRange: &orgreports.TransactionDateRange{From: from, To: to}, WalletIDs: []string{walletID}, IgnoredStatuses: []string{"Unignored"}},
		}
		page, err := client.SearchTransactions(cmd.Context(), orgID, request)
		if err != nil {
			return nil, transactionsRead, pages, false, fmt.Errorf("search transactions for negative balance: %w", err)
		}
		pages++
		transactionsRead += len(page.Transactions)
		for _, transaction := range page.Transactions {
			result = append(result, matchingNegativeLines(transaction, walletID, token, seenLines)...)
		}
		if page.NextToken == "" || page.NextToken == nextToken || len(page.Transactions) == 0 {
			return result, transactionsRead, pages, false, nil
		}
		nextToken = page.NextToken
	}
}

func matchingNegativeLines(raw json.RawMessage, walletID, token string, seen map[string]bool) []negativeLine {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var transaction map[string]any
	if decoder.Decode(&transaction) != nil {
		return nil
	}
	txnID := firstString(transaction["id"], transaction["transactionId"])
	txnTimestamp := firstString(transaction["timestamp"], transaction["dateTime"])
	lineValues, _ := transaction["lines"].([]any)
	if len(lineValues) == 0 {
		lineValues, _ = transaction["txnLines"].([]any)
	}
	result := make([]negativeLine, 0)
	for index, value := range lineValues {
		line, _ := value.(map[string]any)
		if firstString(line["walletId"]) != walletID {
			continue
		}
		lineToken := firstString(line["amountCurrencyName"], line["currencyName"], line["currencySymbol"], transaction["ticker"])
		if !strings.EqualFold(lineToken, token) {
			continue
		}
		amount, ok := decimalAmount(line["amount"])
		if !ok {
			continue
		}
		operation := strings.ToUpper(firstString(line["operation"]))
		sign := operationSign(operation)
		if sign == 0 {
			continue
		}
		if amount.Sign() < 0 {
			amount.Neg(amount)
		}
		if sign < 0 {
			amount.Neg(amount)
		}
		timeText := firstString(line["dateTime"], txnTimestamp)
		if timeText == "" {
			if created, ok := numericInt64(transaction["created"]); ok {
				timeText = time.Unix(created, 0).UTC().Format(time.RFC3339Nano)
			}
		}
		parsedTime, ok := parseTransactionTime(timeText)
		if !ok {
			continue
		}
		lineID := firstString(line["txnLineId"], line["id"], line["line"])
		if lineID == "" {
			lineID = strconv.Itoa(index + 1)
		}
		key := strings.Join([]string{txnID, lineID, operation, amount.RatString(), timeText}, "|")
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, negativeLine{Timestamp: parsedTime, TimeText: parsedTime.Format(time.RFC3339Nano), Operation: operation, Amount: amount, ID: txnID, LineID: lineID})
	}
	return result
}

func decimalAmount(value any) (*big.Rat, bool) {
	if object, ok := value.(map[string]any); ok {
		for _, key := range []string{"value", "amount", "displayValue"} {
			if nested, exists := object[key]; exists {
				if result, valid := decimalAmount(nested); valid {
					return result, true
				}
			}
		}
	}
	text := firstString(value)
	if text == "" {
		return nil, false
	}
	result, ok := new(big.Rat).SetString(text)
	return result, ok
}

func operationSign(operation string) int {
	switch operation {
	case "WITHDRAW", "FEE", "SELL", "SEND", "BRIDGE_OUT", "SWAP_OUT", "DEBIT", "OUT":
		return -1
	case "DEPOSIT", "BUY", "RECEIVE", "BRIDGE_IN", "SWAP_IN", "REWARD", "STAKE_REWARD", "CREDIT", "IN":
		return 1
	default:
		return 0
	}
}

func parseTransactionTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, " UTC", "Z"))
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05Z"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func calculateNegativeBalances(lines []negativeLine) ([]negativeOutputRow, string, string) {
	sort.SliceStable(lines, func(i, j int) bool {
		if !lines[i].Timestamp.Equal(lines[j].Timestamp) {
			return lines[i].Timestamp.Before(lines[j].Timestamp)
		}
		iIn := operationSign(lines[i].Operation) > 0
		jIn := operationSign(lines[j].Operation) > 0
		if iIn != jIn {
			return iIn
		}
		return lines[i].Amount.Cmp(lines[j].Amount) > 0
	})
	balance := new(big.Rat)
	lowest := new(big.Rat)
	firstNegative := ""
	rows := make([]negativeOutputRow, 0, len(lines))
	for index := range lines {
		balance.Add(balance, lines[index].Amount)
		lines[index].Balance = new(big.Rat).Set(balance)
		if balance.Sign() < 0 && firstNegative == "" {
			firstNegative = lines[index].TimeText
		}
		if balance.Cmp(lowest) < 0 {
			lowest.Set(balance)
		}
		rows = append(rows, negativeOutputRow{Timestamp: lines[index].TimeText, Operation: lines[index].Operation, Amount: decimalString(lines[index].Amount), Balance: decimalString(balance), ID: lines[index].ID, LineID: lines[index].LineID})
	}
	return rows, firstNegative, decimalString(lowest)
}

func decimalString(value *big.Rat) string {
	if value == nil {
		return ""
	}
	denominator := new(big.Int).Set(value.Denom())
	two, five, one := big.NewInt(2), big.NewInt(5), big.NewInt(1)
	twos, fives := 0, 0
	for new(big.Int).Mod(denominator, two).Sign() == 0 {
		denominator.Div(denominator, two)
		twos++
	}
	for new(big.Int).Mod(denominator, five).Sign() == 0 {
		denominator.Div(denominator, five)
		fives++
	}
	if denominator.Cmp(one) != 0 {
		return value.RatString()
	}
	digits := max(twos, fives)
	text := value.FloatString(digits)
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	if text == "" || text == "-0" {
		return "0"
	}
	return text
}

func negativeRowsCSV(rows []negativeOutputRow) ([]byte, error) {
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{"timestamp", "operation", "amount", "balance", "id", "lineId"}); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := writer.Write([]string{row.Timestamp, row.Operation, row.Amount, row.Balance, row.ID, row.LineID}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return output.Bytes(), writer.Error()
}
