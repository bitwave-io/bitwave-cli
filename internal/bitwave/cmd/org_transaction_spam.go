package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/addresssvc"
	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

type spamLookup struct {
	RequestedSymbol      string           `json:"requestedSymbol"`
	Status               string           `json:"status"`
	SpamScore            *float64         `json:"spamScore,omitempty"`
	MeetsSpamThreshold   bool             `json:"meetsSpamThreshold"`
	IgnoreRecommendation bool             `json:"ignoreRecommendation"`
	Coin                 *addresssvc.Coin `json:"coin,omitempty"`
	Error                string           `json:"error,omitempty"`
}

type spamAssetPlan struct {
	AssetID                  string     `json:"assetId"`
	AssetSymbol              string     `json:"assetSymbol"`
	Lookup                   spamLookup `json:"lookup"`
	CoinIDMatches            bool       `json:"coinIdMatches"`
	IgnoreTransactionIDs     []string   `json:"ignoreTransactionIds,omitempty"`
	ExcludedMixedTokenCount  int        `json:"excludedMixedTokenCount"`
	ExcludedUnexpectedCount  int        `json:"excludedUnexpectedCount"`
	TransactionPageTruncated bool       `json:"transactionPageTruncated"`
}

type spamAnalyzeFlags struct {
	orgID                                   string
	tickers                                 []string
	concurrency, maxAssets, maxTransactions int
	threshold                               float64
	includeCategorized                      bool
	mutation                                transactionMutationFlags
}

func newTransactionSpamCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spam",
		Short: "Check token spam scores and plan uncategorized transaction ignores",
		Long: `Use Bitwave's address service to check token symbols in bounded,
concurrent batches. A score at or above 0.5 is Bitwave's default spam
threshold. Transaction analysis is uncategorized-only unless explicitly
directed otherwise and excludes every transaction containing a second token.`,
	}
	cmd.AddCommand(newTransactionSpamCheckCmd(), newTransactionSpamAnalyzeCmd(), newTransactionSpamBulkIgnoreCmd())
	return cmd
}

func newTransactionSpamCheckCmd() *cobra.Command {
	var file string
	var concurrency int
	var threshold float64
	cmd := &cobra.Command{
		Use:   "check [SYMBOL...]",
		Short: "Look up many token ticker spam scores concurrently",
		RunE: func(cmd *cobra.Command, args []string) error {
			symbols := append([]string(nil), args...)
			if file != "" {
				fromFile, err := readSpamSymbols(file)
				if err != nil {
					return err
				}
				symbols = append(symbols, fromFile...)
			}
			symbols = normalizedSpamSymbols(symbols)
			if len(symbols) == 0 {
				return errors.New("supply at least one symbol or --file")
			}
			if len(symbols) > 10000 {
				return errors.New("a spam check accepts at most 10000 distinct symbols")
			}
			if err := validateSpamOptions(concurrency, threshold); err != nil {
				return err
			}
			lookups := lookupSpamSymbols(cmd.Context(), addresssvc.New(resolveAddressServiceURL()), symbols, concurrency, threshold)
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": "1", "source": resolveAddressServiceURL(), "threshold": threshold,
				"symbolCount": len(symbols), "results": lookups,
				"policy": "A score at or above the threshold is a spam candidate. Match coin ID and network to organization transaction data before ignoring transactions.",
			})
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "File containing newline- or comma-separated token symbols")
	cmd.Flags().IntVar(&concurrency, "concurrency", 20, "Concurrent address-service lookups (1-100)")
	cmd.Flags().Float64Var(&threshold, "threshold", addresssvc.DefaultSpamThreshold, "Spam-score threshold")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newTransactionSpamAnalyzeCmd() *cobra.Command {
	return newTransactionSpamOrgCmd(false)
}

func newTransactionSpamBulkIgnoreCmd() *cobra.Command {
	return newTransactionSpamOrgCmd(true)
}

func newTransactionSpamOrgCmd(apply bool) *cobra.Command {
	var f spamAnalyzeFlags
	f.concurrency = 20
	f.maxAssets = 5000
	f.maxTransactions = 100
	f.threshold = addresssvc.DefaultSpamThreshold
	f.mutation.timeout = 15 * time.Minute
	use := "analyze"
	short := "Find uncategorized transactions containing confirmed spam tokens"
	if apply {
		use = "bulk-ignore"
		short = "Find and bulk-ignore transactions containing only one confirmed spam token"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateSpamOptions(f.concurrency, f.threshold); err != nil {
				return err
			}
			if f.maxAssets < 1 || f.maxAssets > 10000 {
				return errors.New("--max-assets must be between 1 and 10000")
			}
			if f.maxTransactions < 1 || f.maxTransactions > 100 {
				return errors.New("--max-transactions-per-asset must be between 1 and 100")
			}
			if apply && f.mutation.timeout <= 0 {
				return errors.New("--timeout must be greater than zero")
			}
			resolvedOrg, err := resolveReportOrg(f.orgID)
			if err != nil {
				return err
			}
			var mutation *transactionMutationFlags
			if apply {
				f.mutation.orgID = resolvedOrg
				mutation = &f.mutation
			}
			return runTransactionSpamAnalyze(cmd, resolvedOrg, f.tickers, f.concurrency, f.maxAssets, f.maxTransactions, f.threshold, f.includeCategorized, mutation)
		},
	}
	cmd.Flags().StringVar(&f.orgID, "org", "", "Organization ID override")
	cmd.Flags().StringSliceVar(&f.tickers, "ticker", nil, "Ticker selected from the transaction UI filter (repeatable; skips full-org discovery)")
	cmd.Flags().IntVar(&f.concurrency, "concurrency", f.concurrency, "Concurrent address-service lookups (1-100)")
	cmd.Flags().IntVar(&f.maxAssets, "max-assets", f.maxAssets, "Maximum distinct organization assets to check (1-10000)")
	cmd.Flags().IntVar(&f.maxTransactions, "max-transactions-per-asset", f.maxTransactions, "Maximum ignore-ready transactions returned per spam asset (1-100)")
	cmd.Flags().Float64Var(&f.threshold, "threshold", f.threshold, "Spam-score threshold")
	if !apply {
		cmd.Flags().BoolVar(&f.includeCategorized, "include-categorized", false, "Include categorized transactions in read-only analysis")
	}
	if apply {
		cmd.Flags().BoolVar(&f.mutation.yes, "yes", false, "Confirm the bulk ignore mutation")
		cmd.Flags().BoolVar(&f.mutation.dryRun, "dry-run", false, "Print the exact bulk ignore request without changing the organization")
		cmd.Flags().StringVar(&f.mutation.bulkActionID, "bulk-action-id", "", "Optional idempotency key for the server workflow")
		cmd.Flags().BoolVar(&f.mutation.noWait, "no-wait", false, "Return immediately if the server starts an asynchronous workflow")
		cmd.Flags().DurationVar(&f.mutation.timeout, "timeout", f.mutation.timeout, "Maximum time to wait for the asynchronous workflow")
	}
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func runTransactionSpamAnalyze(cmd *cobra.Command, orgID string, selectedTickers []string, concurrency, maxAssets, maxTransactions int, threshold float64, includeCategorized bool, mutation *transactionMutationFlags) error {
	client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
	client.TransactionServiceURL = strings.TrimRight(resolveTransactionsBaseURL(), "/")
	filters := orgreports.TransactionExportFilters{IgnoredStatuses: []string{"Unignored"}}
	transactionScope := "uncategorized-only"
	if !includeCategorized {
		filters.CategorizationStatuses = []string{"Uncategorized"}
	} else {
		transactionScope = "all-categorization-statuses"
	}
	selectedTickers = normalizedSpamSymbols(selectedTickers)
	if len(selectedTickers) > 0 {
		lookups := lookupSpamSymbols(cmd.Context(), addresssvc.New(resolveAddressServiceURL()), selectedTickers, concurrency, threshold)
		confirmedSpamTickers := []string{}
		for _, lookup := range lookups {
			if lookup.MeetsSpamThreshold && lookup.Coin != nil {
				confirmedSpamTickers = append(confirmedSpamTickers, lookup.RequestedSymbol)
			}
		}
		return runSelectedTickerIgnore(cmd, client, orgID, confirmedSpamTickers, maxTransactions, filters, transactionScope, mutation, lookups)
	}
	tickerValues, err := client.TransactionTickerValues(cmd.Context(), orgID)
	if err != nil {
		return fmt.Errorf("discover transaction tickers from the transaction-grid lookup: %w", err)
	}
	symbols := normalizedSpamSymbols(tickerValues)
	assetsTruncated := len(symbols) > maxAssets
	if assetsTruncated {
		symbols = symbols[:maxAssets]
	}
	lookups := lookupSpamSymbols(cmd.Context(), addresssvc.New(resolveAddressServiceURL()), symbols, concurrency, threshold)
	lookupBySymbol := make(map[string]spamLookup, len(lookups))
	cleanCount := 0
	reviewLookups := []spamLookup{}
	unresolvedLookups := []spamLookup{}
	for _, lookup := range lookups {
		lookupBySymbol[lookup.RequestedSymbol] = lookup
		switch lookup.Status {
		case "clean":
			cleanCount++
		case "review":
			reviewLookups = append(reviewLookups, lookup)
		case "unresolved":
			unresolvedLookups = append(unresolvedLookups, lookup)
		}
	}
	assetNames := map[string]string{}
	assetBySymbol := map[string][]string{}
	assetMappingError := ""
	spamLookupCount := 0
	for _, lookup := range lookups {
		if lookup.MeetsSpamThreshold && lookup.Coin != nil {
			spamLookupCount++
		}
	}
	if spamLookupCount > 0 {
		summaryAssets, summaryErr := client.TransactionSummaryAssets(cmd.Context(), orgID)
		if summaryErr != nil {
			assetMappingError = summaryErr.Error()
		} else {
			assetNames = make(map[string]string, len(summaryAssets))
			for _, asset := range summaryAssets {
				assetNames[asset.AssetID] = asset.AssetName
				normalized := strings.ToUpper(strings.TrimSpace(asset.AssetName))
				if normalized != "" && asset.AssetID != "" {
					assetBySymbol[normalized] = append(assetBySymbol[normalized], asset.AssetID)
				}
			}
		}
	}
	plans := []spamAssetPlan{}
	allIgnoreIDs := []string{}
	unresolvedSpamSymbols := []string{}
	for _, symbol := range symbols {
		lookup := lookupBySymbol[symbol]
		if !lookup.MeetsSpamThreshold || lookup.Coin == nil {
			continue
		}
		if len(assetBySymbol[symbol]) == 0 {
			unresolvedSpamSymbols = append(unresolvedSpamSymbols, symbol)
			continue
		}
		for _, assetID := range assetBySymbol[symbol] {
			coinMatches := assetCoinID(assetID) == lookup.Coin.CoinID && lookup.Coin.CoinID != 0
			plan := spamAssetPlan{AssetID: assetID, AssetSymbol: assetNames[assetID], Lookup: lookup, CoinIDMatches: coinMatches}
			if !coinMatches {
				plans = append(plans, plan)
				continue
			}
			response, searchErr := client.SearchTransactions(cmd.Context(), orgID, orgreports.TransactionSearchRequest{
				Timezone: "UTC", Limit: maxTransactions, SortBy: "timestamp", SortDirection: "desc",
				Filters: orgreports.TransactionExportFilters{
					AssetIDs: []string{assetID}, CategorizationStatuses: filters.CategorizationStatuses, IgnoredStatuses: []string{"Unignored"},
				},
			})
			if searchErr != nil {
				plan.Lookup.Error = "find matching transactions: " + searchErr.Error()
				plans = append(plans, plan)
				continue
			}
			plan.TransactionPageTruncated = response.NextToken != ""
			for _, raw := range response.Transactions {
				var transaction struct {
					ID    string                   `json:"id"`
					Lines []compactTransactionLine `json:"lines"`
				}
				if json.Unmarshal(raw, &transaction) != nil || transaction.ID == "" || len(transaction.Lines) == 0 {
					plan.ExcludedUnexpectedCount++
					continue
				}
				onlySpamAsset := true
				for _, line := range transaction.Lines {
					if line.AmountCurrencyID == "" || line.AmountCurrencyID != assetID {
						onlySpamAsset = false
						break
					}
				}
				if !onlySpamAsset {
					plan.ExcludedMixedTokenCount++
					continue
				}
				plan.IgnoreTransactionIDs = append(plan.IgnoreTransactionIDs, transaction.ID)
				allIgnoreIDs = append(allIgnoreIDs, transaction.ID)
			}
			plans = append(plans, plan)
		}
	}
	allIgnoreIDs = uniqueNonEmpty(allIgnoreIDs)
	sort.Slice(plans, func(i, j int) bool {
		if len(plans[i].IgnoreTransactionIDs) != len(plans[j].IgnoreTransactionIDs) {
			return len(plans[i].IgnoreTransactionIDs) > len(plans[j].IgnoreTransactionIDs)
		}
		return plans[i].AssetID < plans[j].AssetID
	})
	output := map[string]any{
		"schemaVersion": "1", "organization": orgID, "source": resolveAddressServiceURL(), "threshold": threshold,
		"transactionScope": transactionScope, "assetDiscovery": "transaction-grid-ticker-lookup", "assetCount": len(symbols), "assetsTruncated": assetsTruncated,
		"lookupCount": len(lookups), "cleanLookupCount": cleanCount, "reviewLookups": reviewLookups,
		"unresolvedLookups": unresolvedLookups, "unresolvedSpamSymbols": unresolvedSpamSymbols,
		"spamAssetPlans": plans, "ignoreReadyCount": len(allIgnoreIDs), "ignoreTransactionIds": allIgnoreIDs,
		"policy": []string{
			"Only scores at or above the Bitwave spam threshold are ignore candidates.",
			"Coin ID must match the organization asset ID; ticker text alone is insufficient when symbols collide.",
			"A transaction is ignore-ready only when every token-bearing line contains the same confirmed spam asset. Any mixed-token transaction is excluded.",
			"Use `bitwave transaction spam bulk-ignore --yes` to execute this plan. Rerun when transactionPageTruncated is true.",
		},
	}
	if assetMappingError != "" {
		output["assetMappingError"] = assetMappingError
	}
	if mutation == nil {
		return writeJSON(cmd.OutOrStdout(), output)
	}
	request := orgreports.BulkStateRequest{BulkActionID: mutation.bulkActionID, TransactionIDs: allIgnoreIDs, Update: orgreports.TransactionStateIgnore}
	preview := map[string]any{"method": "POST", "path": fmt.Sprintf("/v3/orgs/%s/transactions/bulk-state", orgID), "body": request}
	if mutation.dryRun {
		output["bulkIgnore"] = map[string]any{"status": "preview", "dryRun": true, "request": preview}
		return writeJSON(cmd.OutOrStdout(), output)
	}
	if !mutation.yes {
		return errors.New("refusing to bulk-ignore transactions without --yes (use --dry-run to preview)")
	}
	if len(allIgnoreIDs) == 0 {
		output["bulkIgnore"] = map[string]any{"status": "noop", "processed": 0}
		return writeJSON(cmd.OutOrStdout(), output)
	}
	const bulkStateBatchSize = 100
	batchResults := []*orgreports.BulkStateResponse{}
	totalProcessed := 0
	totalSucceeded := 0
	allSucceeded := true
	allFailed := []orgreports.TransactionFailure{}
	for start := 0; start < len(allIgnoreIDs); start += bulkStateBatchSize {
		end := min(start+bulkStateBatchSize, len(allIgnoreIDs))
		batchActionID := mutation.bulkActionID
		if batchActionID != "" && len(allIgnoreIDs) > bulkStateBatchSize {
			batchActionID = fmt.Sprintf("%s-%d", batchActionID, start/bulkStateBatchSize+1)
		}
		batchRequest := orgreports.BulkStateRequest{
			BulkActionID: batchActionID, TransactionIDs: allIgnoreIDs[start:end], Update: orgreports.TransactionStateIgnore,
		}
		result, err := client.BulkUpdateTransactionState(cmd.Context(), orgID, batchRequest)
		if err != nil {
			return fmt.Errorf("bulk-ignore scored spam-token transaction batch %d: %w", start/bulkStateBatchSize+1, err)
		}
		if result.WorkflowID != "" && !mutation.noWait && strings.EqualFold(result.Status, "RUNNING") {
			ctx, cancel := context.WithTimeout(cmd.Context(), mutation.timeout)
			result, err = waitForBulkStateWorkflow(ctx, client, orgID, result.WorkflowID)
			cancel()
			if err != nil {
				return err
			}
		}
		batchResults = append(batchResults, result)
		totalProcessed += result.Processed
		totalSucceeded += result.SuccessCount
		allFailed = append(allFailed, result.Failed...)
		if !result.Success && !(mutation.noWait && strings.EqualFold(result.Status, "RUNNING")) {
			allSucceeded = false
		}
	}
	output["bulkIgnore"] = map[string]any{
		"success": allSucceeded, "processed": totalProcessed, "successCount": totalSucceeded,
		"failed": allFailed, "batchCount": len(batchResults), "batches": batchResults,
	}
	if err := writeJSON(cmd.OutOrStdout(), output); err != nil {
		return err
	}
	if !allSucceeded {
		return fmt.Errorf("bulk ignore processed %d transaction(s): %d succeeded, %d failed", totalProcessed, totalSucceeded, len(allFailed))
	}
	return nil
}

func runSelectedTickerIgnore(cmd *cobra.Command, client *orgreports.Client, orgID string, symbols []string, maxTransactions int, filters orgreports.TransactionExportFilters, transactionScope string, mutation *transactionMutationFlags, scoreLookups []spamLookup) error {
	// Explicit ticker selection mirrors the transaction-grid UI. Mutations stay
	// uncategorized-only even if a caller accidentally supplies
	// --include-categorized; categorized work must never be swept up by a spam
	// convenience command.
	filters.CategorizationStatuses = []string{"Uncategorized"}
	transactionScope = "uncategorized-only"
	plans := make([]map[string]any, 0, len(symbols))
	allIgnoreIDs := []string{}
	for _, symbol := range symbols {
		response, err := client.SearchTransactions(cmd.Context(), orgID, orgreports.TransactionSearchRequest{
			Timezone: "UTC", Limit: maxTransactions, SortBy: "timestamp", SortDirection: "desc",
			Filters: orgreports.TransactionExportFilters{
				AmountCurrencyNames:    []string{symbol},
				CategorizationStatuses: filters.CategorizationStatuses,
				IgnoredStatuses:        []string{"Unignored"},
			},
		})
		plan := map[string]any{
			"ticker": symbol, "filter": "amountCurrencyNames", "ignoreTransactionIds": []string{},
			"excludedMixedTokenCount": 0, "excludedUnexpectedCount": 0,
			"transactionPageTruncated": false,
		}
		if err != nil {
			plan["error"] = err.Error()
			plans = append(plans, plan)
			continue
		}
		plan["transactionPageTruncated"] = response.NextToken != ""
		ignoreIDs := []string{}
		excludedMixed := 0
		excludedUnexpected := 0
		for _, raw := range response.Transactions {
			var transaction struct {
				ID    string                   `json:"id"`
				Lines []compactTransactionLine `json:"lines"`
			}
			if json.Unmarshal(raw, &transaction) != nil || transaction.ID == "" || len(transaction.Lines) == 0 {
				excludedUnexpected++
				continue
			}
			onlySelectedTicker := true
			for _, line := range transaction.Lines {
				if strings.ToUpper(strings.TrimSpace(line.AmountCurrencyName)) != symbol {
					onlySelectedTicker = false
					break
				}
			}
			if !onlySelectedTicker {
				excludedMixed++
				continue
			}
			ignoreIDs = append(ignoreIDs, transaction.ID)
			allIgnoreIDs = append(allIgnoreIDs, transaction.ID)
		}
		plan["ignoreTransactionIds"] = ignoreIDs
		plan["excludedMixedTokenCount"] = excludedMixed
		plan["excludedUnexpectedCount"] = excludedUnexpected
		plans = append(plans, plan)
	}
	allIgnoreIDs = uniqueNonEmpty(allIgnoreIDs)
	output := map[string]any{
		"schemaVersion": "1", "organization": orgID,
		"transactionScope": transactionScope,
		"assetDiscovery":   "explicit-transaction-grid-ticker-filter",
		"selectedTickers": func() []string {
			selected := make([]string, 0, len(scoreLookups))
			for _, lookup := range scoreLookups {
				selected = append(selected, lookup.RequestedSymbol)
			}
			return selected
		}(),
		"scoreLookups":         scoreLookups,
		"confirmedSpamTickers": symbols,
		"tickerPlans":          plans,
		"ignoreReadyCount":     len(allIgnoreIDs),
		"ignoreTransactionIds": allIgnoreIDs,
		"policy": []string{
			"Ticker values are sent through the same amountCurrencyNames filter used by the transaction UI.",
			"Every selected ticker must meet the configured address-service spam-score threshold before transaction selection.",
			"Only Uncategorized and Unignored transactions are eligible.",
			"Any transaction containing a different or unidentified token line is excluded.",
		},
	}
	if mutation == nil {
		return writeJSON(cmd.OutOrStdout(), output)
	}
	request := orgreports.BulkStateRequest{BulkActionID: mutation.bulkActionID, TransactionIDs: allIgnoreIDs, Update: orgreports.TransactionStateIgnore}
	preview := map[string]any{"method": "POST", "path": fmt.Sprintf("/v3/orgs/%s/transactions/bulk-state", orgID), "body": request}
	if mutation.dryRun {
		output["bulkIgnore"] = map[string]any{"status": "preview", "dryRun": true, "request": preview}
		return writeJSON(cmd.OutOrStdout(), output)
	}
	if !mutation.yes {
		return errors.New("refusing to bulk-ignore transactions without --yes (use --dry-run to preview)")
	}
	if len(allIgnoreIDs) == 0 {
		output["bulkIgnore"] = map[string]any{"status": "noop", "processed": 0}
		return writeJSON(cmd.OutOrStdout(), output)
	}
	const bulkStateBatchSize = 100
	batchResults := []*orgreports.BulkStateResponse{}
	totalProcessed := 0
	totalSucceeded := 0
	allSucceeded := true
	allFailed := []orgreports.TransactionFailure{}
	for start := 0; start < len(allIgnoreIDs); start += bulkStateBatchSize {
		end := min(start+bulkStateBatchSize, len(allIgnoreIDs))
		batchActionID := mutation.bulkActionID
		if batchActionID != "" && len(allIgnoreIDs) > bulkStateBatchSize {
			batchActionID = fmt.Sprintf("%s-%d", batchActionID, start/bulkStateBatchSize+1)
		}
		batchRequest := orgreports.BulkStateRequest{
			BulkActionID: batchActionID, TransactionIDs: allIgnoreIDs[start:end], Update: orgreports.TransactionStateIgnore,
		}
		result, err := client.BulkUpdateTransactionState(cmd.Context(), orgID, batchRequest)
		if err != nil {
			return fmt.Errorf("bulk-ignore UI-filtered spam-token transaction batch %d: %w", start/bulkStateBatchSize+1, err)
		}
		if result.WorkflowID != "" && !mutation.noWait && strings.EqualFold(result.Status, "RUNNING") {
			ctx, cancel := context.WithTimeout(cmd.Context(), mutation.timeout)
			result, err = waitForBulkStateWorkflow(ctx, client, orgID, result.WorkflowID)
			cancel()
			if err != nil {
				return err
			}
		}
		batchResults = append(batchResults, result)
		totalProcessed += result.Processed
		totalSucceeded += result.SuccessCount
		allFailed = append(allFailed, result.Failed...)
		if !result.Success && !(mutation.noWait && strings.EqualFold(result.Status, "RUNNING")) {
			allSucceeded = false
		}
	}
	output["bulkIgnore"] = map[string]any{
		"success": allSucceeded, "processed": totalProcessed, "successCount": totalSucceeded,
		"failed": allFailed, "batchCount": len(batchResults), "batches": batchResults,
	}
	if err := writeJSON(cmd.OutOrStdout(), output); err != nil {
		return err
	}
	if !allSucceeded {
		return fmt.Errorf("bulk ignore processed %d transaction(s): %d succeeded, %d failed", totalProcessed, totalSucceeded, len(allFailed))
	}
	return nil
}

func lookupSpamSymbols(ctx context.Context, client *addresssvc.Client, symbols []string, concurrency int, threshold float64) []spamLookup {
	results := make([]spamLookup, len(symbols))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(concurrency, len(symbols)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				symbol := symbols[index]
				coin, err := client.LookupSymbol(ctx, symbol)
				result := spamLookup{RequestedSymbol: symbol}
				if err != nil {
					result.Status = "unresolved"
					result.Error = err.Error()
				} else {
					result.Coin = coin
					result.SpamScore = coin.SpamScore
					result.MeetsSpamThreshold = addresssvc.IsSpam(coin, threshold)
					result.IgnoreRecommendation = result.MeetsSpamThreshold
					switch {
					case result.MeetsSpamThreshold:
						result.Status = "spam"
					case coin.SpamScore != nil && *coin.SpamScore > 0:
						result.Status = "review"
					default:
						result.Status = "clean"
					}
				}
				results[index] = result
			}
		}()
	}
	for index := range symbols {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return results
}

func validateSpamOptions(concurrency int, threshold float64) error {
	if concurrency < 1 || concurrency > 100 {
		return errors.New("--concurrency must be between 1 and 100")
	}
	if threshold < 0 {
		return errors.New("--threshold must be zero or greater")
	}
	return nil
}

func readSpamSymbols(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open symbol file: %w", err)
	}
	defer func() { _ = file.Close() }()
	result := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		result = append(result, strings.FieldsFunc(scanner.Text(), func(r rune) bool { return r == ',' || r == '\t' || r == ' ' })...)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read symbol file: %w", err)
	}
	return result, nil
}

func normalizedSpamSymbols(values []string) []string {
	for index := range values {
		values[index] = strings.ToUpper(strings.TrimSpace(values[index]))
	}
	return uniqueNonEmpty(values)
}

func assetCoinID(assetID string) int64 {
	value := strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(assetID)), "COIN.")
	coinID, _ := strconv.ParseInt(value, 10, 64)
	return coinID
}
