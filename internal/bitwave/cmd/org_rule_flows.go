package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

type flowAnalysisFlags struct {
	orgID, direction, source, from, to, nextToken string
	wallets                                       []string
	maxTransactions, minCount, limit              int
	includeCategorized, includeIgnored            bool
}

type flowCluster struct {
	ID                   string                   `json:"id"`
	Source               string                   `json:"source"`
	Direction            string                   `json:"direction"`
	TransactionType      string                   `json:"transactionType"`
	AssetID              string                   `json:"assetId,omitempty"`
	Asset                string                   `json:"asset,omitempty"`
	CounterpartyAddress  string                   `json:"counterpartyAddress,omitempty"`
	FromAddresses        []string                 `json:"fromAddresses,omitempty"`
	ToAddresses          []string                 `json:"toAddresses,omitempty"`
	WalletIDs            []string                 `json:"walletIds,omitempty"`
	Count                int                      `json:"count"`
	EvidenceCount        int                      `json:"evidenceCount"`
	EvidenceLimit        int                      `json:"evidenceLimit"`
	EvidenceSufficient   bool                     `json:"evidenceSufficient"`
	ComplexCount         int                      `json:"complexCount"`
	FirstSeen            string                   `json:"firstSeen,omitempty"`
	LastSeen             string                   `json:"lastSeen,omitempty"`
	SampleTransactionIDs []string                 `json:"sampleTransactionIds,omitempty"`
	ConditionCandidates  []ruleConditionCandidate `json:"conditionCandidates,omitempty"`
	SuggestedRule        agentRuleSpec            `json:"suggestedRule"`
	Question             string                   `json:"question"`
	Advisories           []string                 `json:"advisories,omitempty"`
}

type flowClusterAccumulator struct {
	cluster      flowCluster
	from         map[string]bool
	to           map[string]bool
	wallets      map[string]bool
	transactions []compactTransaction
}

type flowTransaction struct {
	ID                   string                   `json:"id"`
	Timestamp            string                   `json:"timestamp"`
	TransactionType      string                   `json:"transactionType"`
	MethodID             string                   `json:"methodId"`
	Metadata             map[string]any           `json:"metadata"`
	CategorizationStatus string                   `json:"categorizationStatus"`
	Ignored              bool                     `json:"ignored"`
	Lines                []compactTransactionLine `json:"lines"`
}

func newRuleFlowsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flows",
		Short: "Cluster uncategorized inflows and outflows into compact rule evidence",
		Long: `Analyze recurring inflow and outflow signatures without sending raw history
to an LLM. Addresses are always emitted as complete exact values and are never
shortened with ellipses. Direction alone does not select an accounting action.`,
	}
	cmd.AddCommand(newRuleFlowsAnalyzeCmd())
	return cmd
}

func newRuleFlowsAnalyzeCmd() *cobra.Command {
	var f flowAnalysisFlags
	f.direction = "all"
	f.source = "auto"
	f.maxTransactions = 500
	f.minCount = 2
	f.limit = 50
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Find recurring uncategorized inflow/outflow clusters",
		RunE:  func(cmd *cobra.Command, _ []string) error { return runRuleFlowsAnalyze(cmd, f) },
	}
	cmd.Flags().StringVar(&f.orgID, "org", "", "Organization ID override")
	cmd.Flags().StringVar(&f.direction, "direction", f.direction, "inflow, outflow, or all")
	cmd.Flags().StringVar(&f.source, "source", f.source, "Discovery source: auto, summary, or transactions")
	cmd.Flags().StringVar(&f.from, "from", "", "Inclusive start date (YYYY-MM-DD; requires --to)")
	cmd.Flags().StringVar(&f.to, "to", "", "Inclusive end date (YYYY-MM-DD; requires --from)")
	cmd.Flags().StringSliceVar(&f.wallets, "wallet", nil, "Wallet ID or exact name (repeatable); omitted analyzes all wallets")
	cmd.Flags().StringVar(&f.nextToken, "next-token", "", "Resume from an earlier truncated analysis")
	cmd.Flags().IntVar(&f.maxTransactions, "max-transactions", f.maxTransactions, "Maximum transactions to scan (1-10000)")
	cmd.Flags().IntVar(&f.minCount, "min-count", f.minCount, "Minimum transactions in a returned cluster")
	cmd.Flags().IntVar(&f.limit, "limit", f.limit, "Maximum clusters to return (1-200)")
	cmd.Flags().BoolVar(&f.includeCategorized, "include-categorized", false, "Include transactions already categorized")
	cmd.Flags().BoolVar(&f.includeIgnored, "include-ignored", false, "Include ignored transactions")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func runRuleFlowsAnalyze(cmd *cobra.Command, f flowAnalysisFlags) error {
	direction := strings.ToLower(strings.TrimSpace(f.direction))
	if direction != "all" && direction != "inflow" && direction != "outflow" {
		return errors.New("--direction must be inflow, outflow, or all")
	}
	source := strings.ToLower(strings.TrimSpace(f.source))
	if source != "auto" && source != "summary" && source != "transactions" {
		return errors.New("--source must be auto, summary, or transactions")
	}
	if f.maxTransactions < 1 || f.maxTransactions > 10000 {
		return errors.New("--max-transactions must be between 1 and 10000")
	}
	if f.minCount < 1 {
		return errors.New("--min-count must be at least 1")
	}
	if f.limit < 1 || f.limit > 200 {
		return errors.New("--limit must be between 1 and 200")
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
	summaryEligible := f.from == "" && len(f.wallets) == 0 && f.nextToken == "" && !f.includeIgnored
	if source == "summary" && !summaryEligible {
		return errors.New("--source summary does not support date, wallet, next-token, or include-ignored filters; use --source transactions")
	}
	warnings := []string{}
	if source != "transactions" && summaryEligible {
		records, summaryErr := client.TransactionSummaryAddresses(cmd.Context(), orgID, 1, 100)
		if summaryErr == nil {
			clusters := clusterSummaryAddresses(records, direction, f.includeCategorized, f.minCount, f.limit)
			return writeFlowAnalysis(cmd, orgID, "transaction-summary", len(records), 0, clusters, "", f.includeCategorized, warnings)
		}
		if source == "summary" {
			return fmt.Errorf("load Bitwave Transaction Summary: %w", summaryErr)
		}
		warnings = append(warnings, "Bitwave Transaction Summary was unavailable; the CLI used bounded raw transaction search instead: "+summaryErr.Error())
	}
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
	types := []string{"receive", "send"}
	if direction == "inflow" {
		types = []string{"receive"}
	} else if direction == "outflow" {
		types = []string{"send"}
	}
	filters := orgreports.TransactionExportFilters{
		DateRange: optionalDateRange(f.from, f.to), WalletIDs: walletIDs, TransactionTypes: types,
	}
	if !f.includeCategorized {
		filters.CategorizationStatuses = []string{"Uncategorized"}
	}
	items := make([]json.RawMessage, 0, min(f.maxTransactions, 500))
	next := strings.TrimSpace(f.nextToken)
	lastToken := ""
	for len(items) < f.maxTransactions {
		pageLimit := min(100, f.maxTransactions-len(items))
		response, searchErr := client.SearchTransactions(cmd.Context(), orgID, orgreports.TransactionSearchRequest{
			Timezone: org.Timezone, Limit: pageLimit, NextToken: next, SortBy: "timestamp", SortDirection: "desc", Filters: filters,
		})
		if searchErr != nil {
			return fmt.Errorf("search flow transactions after %d rows: %w", len(items), searchErr)
		}
		items = append(items, response.Transactions...)
		lastToken = response.NextToken
		if response.NextToken == "" || response.NextToken == next || len(response.Transactions) == 0 {
			lastToken = ""
			break
		}
		next = response.NextToken
	}
	clusters, skipped := clusterFlowTransactions(items, f.includeIgnored, f.minCount, f.limit)
	return writeFlowAnalysis(cmd, orgID, "transaction-search", len(items), skipped, clusters, lastToken, f.includeCategorized, warnings)
}

func writeFlowAnalysis(cmd *cobra.Command, orgID, source string, scanned, skipped int, clusters []flowCluster, nextToken string, includeCategorized bool, warnings []string) error {
	transactionScope := "uncategorized-only"
	if includeCategorized {
		transactionScope = "all-categorization-statuses"
	}
	return writeJSON(cmd.OutOrStdout(), map[string]any{
		"schemaVersion": "1", "organization": orgID, "source": source, "scanned": scanned, "skipped": skipped,
		"clusterCount": len(clusters), "clusters": clusters, "truncated": nextToken != "", "nextToken": nextToken,
		"warnings":          warnings,
		"transactionScope":  transactionScope,
		"addressPolicy":     "All addresses are complete exact values; the CLI never emits abbreviated 0x123... rule conditions.",
		"walletScopePolicy": "Simple inflow and outflow rules should include a wallet by default. Flow analysis clusters per wallet and includes its stable ID; organization-wide scope is a deliberate exception.",
		"evidencePolicy":    "By default, only uncategorized transactions count as evidence. One hundred matching uncategorized transactions is sufficient; exhaustive history review is not required.",
		"workflow":          []string{"Start with the highest-count recurring uncategorized counterparties within each wallet.", "Keep the suggested walletId unless the user deliberately requests broader scope.", "Ask the cluster question and let the user choose the accounting treatment.", "Inspect up to 100 representative uncategorized transactions only when the aggregate is ambiguous.", "Resolve only relevant category/contact choices.", "Use rule plan to preview exact conditions and representative matches.", "Apply approved rules in one batch, run rules, then analyze remaining uncategorized flows."},
	})
}

func clusterSummaryAddresses(records []orgreports.TransactionSummaryAddressRecord, direction string, includeCategorized bool, minCount, limit int) []flowCluster {
	observed := map[string]*flowClusterAccumulator{}
	for _, record := range records {
		address := strings.TrimSpace(record.InteractingAddress)
		if address == "" {
			continue
		}
		counts := map[string]int{
			"inflow":  record.DepositsUncategorized,
			"outflow": record.WithdrawalsUncategorized,
		}
		if includeCategorized {
			counts["inflow"] = record.DepositsTransactionCount
			counts["outflow"] = record.WithdrawalsTransactionCount
		}
		for _, flow := range []string{"inflow", "outflow"} {
			if direction != "all" && direction != flow || counts[flow] == 0 {
				continue
			}
			identity := strings.Join([]string{flow, record.WalletID, normalizeRuleAddress(address)}, "\x00")
			acc := observed[identity]
			if acc == nil {
				hash := sha256.Sum256([]byte(identity))
				preset := "simple-inflow"
				typeName := "receive"
				if flow == "outflow" {
					preset, typeName = "simple-outflow", "send"
				}
				acc = &flowClusterAccumulator{
					cluster: flowCluster{
						ID: "flow_" + hex.EncodeToString(hash[:8]), Source: "transaction-summary", Direction: flow,
						TransactionType: typeName, CounterpartyAddress: address,
						SuggestedRule: agentRuleSpec{Preset: preset, Direction: map[string]string{"inflow": "Inbound", "outflow": "Outbound"}[flow]},
					},
					from: map[string]bool{}, to: map[string]bool{}, wallets: map[string]bool{},
				}
				if flow == "inflow" {
					acc.cluster.SuggestedRule.FromAddress = address
				} else {
					acc.cluster.SuggestedRule.ToAddress = address
				}
				observed[identity] = acc
			}
			acc.cluster.Count += counts[flow]
			if record.WalletID != "" {
				acc.wallets[record.WalletID] = true
			}
		}
	}
	result := make([]flowCluster, 0, len(observed))
	for _, acc := range observed {
		if acc.cluster.Count < minCount {
			continue
		}
		acc.cluster.WalletIDs = sortedTrueKeys(acc.wallets)
		completeFlowEvidence(&acc.cluster)
		acc.cluster.Advisories = append(acc.cluster.Advisories,
			"This cluster comes from Bitwave Transaction Summary. Inspect representative transactions only if the counterparty's accounting meaning is unclear.",
			"Direction is evidence, not an accounting conclusion. The user selects the treatment; CLI guidance remains advisory.",
		)
		result = append(result, acc.cluster)
	}
	sortFlowClusters(result)
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func clusterFlowTransactions(items []json.RawMessage, includeIgnored bool, minCount, limit int) ([]flowCluster, int) {
	observed := map[string]*flowClusterAccumulator{}
	skipped := 0
	for _, raw := range items {
		var transaction flowTransaction
		if json.Unmarshal(raw, &transaction) != nil || transaction.ID == "" {
			skipped++
			continue
		}
		if transaction.Ignored && !includeIgnored {
			skipped++
			continue
		}
		direction := flowDirection(transaction.TransactionType)
		line := primaryFlowLine(transaction.Lines, direction)
		if direction == "" || line == nil {
			skipped++
			continue
		}
		counterparty := line.From
		preset := "simple-inflow"
		if direction == "outflow" {
			counterparty = line.To
			preset = "simple-outflow"
		}
		identity := strings.Join([]string{direction, line.WalletID, line.AmountCurrencyID, normalizeRuleAddress(counterparty)}, "\x00")
		acc := observed[identity]
		if acc == nil {
			hash := sha256.Sum256([]byte(identity))
			acc = &flowClusterAccumulator{
				cluster: flowCluster{
					ID: "flow_" + hex.EncodeToString(hash[:8]), Source: "transaction-search", Direction: direction, TransactionType: transaction.TransactionType,
					AssetID: line.AmountCurrencyID, Asset: line.AmountCurrencyName, CounterpartyAddress: counterparty,
					SuggestedRule: agentRuleSpec{Preset: preset, Asset: line.AmountCurrencyName, Direction: map[string]string{"inflow": "Inbound", "outflow": "Outbound"}[direction]},
				},
				from: map[string]bool{}, to: map[string]bool{}, wallets: map[string]bool{},
			}
			if direction == "inflow" {
				acc.cluster.SuggestedRule.FromAddress = counterparty
			} else {
				acc.cluster.SuggestedRule.ToAddress = counterparty
			}
			observed[identity] = acc
		}
		acc.cluster.Count++
		if len(transaction.Lines) > 1 {
			acc.cluster.ComplexCount++
		}
		if acc.cluster.FirstSeen == "" || transaction.Timestamp < acc.cluster.FirstSeen {
			acc.cluster.FirstSeen = transaction.Timestamp
		}
		if transaction.Timestamp > acc.cluster.LastSeen {
			acc.cluster.LastSeen = transaction.Timestamp
		}
		acc.from[line.From] = line.From != ""
		acc.to[line.To] = line.To != ""
		acc.wallets[line.WalletID] = line.WalletID != ""
		if len(acc.cluster.SampleTransactionIDs) < 5 {
			acc.cluster.SampleTransactionIDs = append(acc.cluster.SampleTransactionIDs, transaction.ID)
		}
		if len(acc.transactions) < 100 {
			acc.transactions = append(acc.transactions, compactTransaction{
				ID: transaction.ID, Timestamp: transaction.Timestamp, TransactionType: transaction.TransactionType,
				MethodID: transaction.MethodID, Metadata: transaction.Metadata, LineCount: len(transaction.Lines), Lines: transaction.Lines,
			})
		}
	}
	result := make([]flowCluster, 0, len(observed))
	for _, acc := range observed {
		if acc.cluster.Count < minCount {
			continue
		}
		acc.cluster.FromAddresses = sortedTrueKeys(acc.from)
		acc.cluster.ToAddresses = sortedTrueKeys(acc.to)
		acc.cluster.WalletIDs = sortedTrueKeys(acc.wallets)
		acc.cluster.ConditionCandidates = ruleConditionCandidates(acc.transactions, 10)
		completeFlowEvidence(&acc.cluster)
		if acc.cluster.CounterpartyAddress == "" {
			acc.cluster.Advisories = append(acc.cluster.Advisories, "No counterparty address was available; require another stable condition before proposing a reusable rule.")
		}
		if acc.cluster.ComplexCount > 0 {
			acc.cluster.Advisories = append(acc.cluster.Advisories, fmt.Sprintf("%d matching transactions are multi-line. Explain that they may need DeFi or another multi-sided treatment in Bitwave before applying a simple flow rule.", acc.cluster.ComplexCount))
		}
		acc.cluster.Advisories = append(acc.cluster.Advisories, "Direction is evidence, not an accounting conclusion. The user selects the treatment; CLI guidance remains advisory.")
		result = append(result, acc.cluster)
	}
	sortFlowClusters(result)
	if len(result) > limit {
		result = result[:limit]
	}
	return result, skipped
}

func completeFlowEvidence(cluster *flowCluster) {
	cluster.EvidenceLimit = 100
	cluster.EvidenceCount = min(cluster.Count, cluster.EvidenceLimit)
	cluster.EvidenceSufficient = cluster.Count >= cluster.EvidenceLimit
	if len(cluster.WalletIDs) == 1 {
		cluster.SuggestedRule.WalletID = cluster.WalletIDs[0]
		cluster.Advisories = append(cluster.Advisories, "The suggested rule is wallet-scoped. Retain walletId unless the user deliberately requests an organization-wide simple flow rule.")
		cluster.Question = fmt.Sprintf("What does the %s activity with counterparty %s in wallet %s represent, and which category and contact should apply?", cluster.Direction, cluster.CounterpartyAddress, cluster.WalletIDs[0])
		return
	}
	cluster.Advisories = append(cluster.Advisories, "No unique wallet could be resolved. Do not apply this simple flow rule until wallet scope is selected or organization-wide scope is explicitly intended.")
	cluster.Question = fmt.Sprintf("What does the %s activity with counterparty %s represent, which wallet should the rule cover, and which category and contact should apply?", cluster.Direction, cluster.CounterpartyAddress)
}

func sortFlowClusters(result []flowCluster) {
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].ID < result[j].ID
	})
}

func flowDirection(transactionType string) string {
	switch strings.ToLower(strings.TrimSpace(transactionType)) {
	case "receive", "deposit", "inflow":
		return "inflow"
	case "send", "withdrawal", "outflow":
		return "outflow"
	default:
		return ""
	}
}

func primaryFlowLine(lines []compactTransactionLine, direction string) *compactTransactionLine {
	operation := map[string]string{"inflow": "deposit", "outflow": "withdraw"}[direction]
	for i := range lines {
		if strings.EqualFold(lines[i].Operation, operation) {
			return &lines[i]
		}
	}
	if len(lines) > 0 {
		return &lines[0]
	}
	return nil
}

func normalizeRuleAddress(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "0x") {
		return strings.ToLower(value)
	}
	return value
}

func sortedTrueKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for key, include := range values {
		if include {
			result = append(result, key)
		}
	}
	sort.Strings(result)
	return result
}
