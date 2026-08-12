package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

type metadataAnalysisFlags struct {
	orgID, from, to, nextToken         string
	wallets                            []string
	maxTransactions, candidateLimit    int
	includeCategorized, includeIgnored bool
}

func newRuleMetadataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metadata",
		Short: "Discover reusable metadata and method-ID rule conditions",
		Long: `Profile bounded transaction evidence across every network and transaction
type. The analyzer preserves exact metadata values and ranks repeated conditions;
it never selects an accounting category or contact.`,
	}
	cmd.AddCommand(newRuleMetadataAnalyzeCmd())
	return cmd
}

func newRuleMetadataAnalyzeCmd() *cobra.Command {
	var f metadataAnalysisFlags
	f.maxTransactions = 500
	f.candidateLimit = 50
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Profile uncategorized transactions for reusable metadata patterns",
		RunE:  func(cmd *cobra.Command, _ []string) error { return runRuleMetadataAnalyze(cmd, f) },
	}
	cmd.Flags().StringVar(&f.orgID, "org", "", "Organization ID override")
	cmd.Flags().StringVar(&f.from, "from", "", "Inclusive start date (YYYY-MM-DD; requires --to)")
	cmd.Flags().StringVar(&f.to, "to", "", "Inclusive end date (YYYY-MM-DD; requires --from)")
	cmd.Flags().StringSliceVar(&f.wallets, "wallet", nil, "Wallet ID or exact name (repeatable); omitted analyzes all wallets")
	cmd.Flags().StringVar(&f.nextToken, "next-token", "", "Resume from an earlier truncated analysis")
	cmd.Flags().IntVar(&f.maxTransactions, "max-transactions", f.maxTransactions, "Maximum transactions to scan (1-10000)")
	cmd.Flags().IntVar(&f.candidateLimit, "candidate-limit", f.candidateLimit, "Maximum ranked candidates to return (1-200)")
	cmd.Flags().BoolVar(&f.includeCategorized, "include-categorized", false, "Include transactions already categorized")
	cmd.Flags().BoolVar(&f.includeIgnored, "include-ignored", false, "Include ignored transactions")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func runRuleMetadataAnalyze(cmd *cobra.Command, f metadataAnalysisFlags) error {
	if f.maxTransactions < 1 || f.maxTransactions > 10000 {
		return errors.New("--max-transactions must be between 1 and 10000")
	}
	if f.candidateLimit < 1 || f.candidateLimit > 200 {
		return errors.New("--candidate-limit must be between 1 and 200")
	}
	if (f.from == "") != (f.to == "") {
		return errors.New("--from and --to must be supplied together")
	}
	if f.from != "" {
		if err := validateExportDateRange(f.from, f.to, false); err != nil {
			return err
		}
	}
	orgID, err := resolveReportOrg(f.orgID)
	if err != nil {
		return err
	}
	client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
	org, err := client.Org(cmd.Context(), orgID)
	if err != nil {
		return fmt.Errorf("load organization settings: %w", err)
	}
	wallets, err := client.Wallets(cmd.Context(), orgID)
	if err != nil {
		return fmt.Errorf("discover wallets: %w", err)
	}
	walletIDs, err := resolveWalletRefs(f.wallets, wallets)
	if err != nil {
		return err
	}
	filters := orgreports.TransactionExportFilters{DateRange: optionalDateRange(f.from, f.to), WalletIDs: walletIDs}
	if !f.includeCategorized {
		filters.CategorizationStatuses = []string{"Uncategorized"}
	}
	items := make([]compactTransaction, 0, min(f.maxTransactions, 500))
	scanned := 0
	next := strings.TrimSpace(f.nextToken)
	lastToken := ""
	ignoredSkipped := 0
	for scanned < f.maxTransactions {
		pageLimit := min(100, f.maxTransactions-scanned)
		response, searchErr := client.SearchTransactions(cmd.Context(), orgID, orgreports.TransactionSearchRequest{
			Timezone: org.Timezone, Limit: pageLimit, NextToken: next, SortBy: "timestamp", SortDirection: "desc", Filters: filters,
		})
		if searchErr != nil {
			return fmt.Errorf("search metadata evidence after %d scanned rows: %w", scanned, searchErr)
		}
		scanned += len(response.Transactions)
		for _, transaction := range compactTransactions(response.Transactions) {
			if transaction.Ignored && !f.includeIgnored {
				ignoredSkipped++
				continue
			}
			items = append(items, transaction)
		}
		lastToken = response.NextToken
		if response.NextToken == "" || response.NextToken == next || len(response.Transactions) == 0 {
			lastToken = ""
			break
		}
		next = response.NextToken
	}
	candidates := ruleConditionCandidates(items, f.candidateLimit)
	metadataTransactions, methodTransactions := 0, 0
	for _, transaction := range items {
		if len(transaction.Metadata) > 0 {
			metadataTransactions++
		}
		if strings.TrimSpace(transaction.MethodID) != "" {
			methodTransactions++
		}
	}
	scope := "uncategorized-only"
	if f.includeCategorized {
		scope = "all-categorization-statuses"
	}
	return writeJSON(cmd.OutOrStdout(), map[string]any{
		"schemaVersion": "1", "organization": orgID, "transactionScope": scope,
		"scanned": scanned, "eligible": len(items), "ignoredSkipped": ignoredSkipped, "metadataTransactions": metadataTransactions,
		"methodIdTransactions": methodTransactions, "candidateCount": len(candidates), "conditionCandidates": candidates,
		"truncated": lastToken != "", "nextToken": lastToken,
		"policy": []string{
			"Use repeated stable metadata or methodId conditions across any network or transaction type.",
			"Preserve exact metadata key/value spelling; do not normalize punctuation, case, underscores, or hyphens.",
			"Avoid hashes, IDs, nonces, timestamps, block fields, and other high-cardinality transaction-specific values.",
			"Review observed wallet, transaction-type, network, and asset scopes before deciding whether a narrowing condition is needed.",
			"Metadata identifies activity; the user-approved accounting treatment still determines category and contact.",
			"Validate at least one expected match and one expected non-match before enabling a broad rule.",
		},
	})
}
