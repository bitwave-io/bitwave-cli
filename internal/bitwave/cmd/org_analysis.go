package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func newOrgInfoCmd() *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use: "info", Short: "Show complete active-organization settings", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			organization, err := client.Org(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("get organization info: %w", err)
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": organization})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgUsersCmd() *cobra.Command {
	var orgID string
	var limit int
	cmd := &cobra.Command{
		Use: "users", Aliases: []string{"principals"}, Short: "List organization users, roles, and last login", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			page, err := client.OrgPrincipals(cmd.Context(), resolvedOrg, limit)
			if err != nil {
				return fmt.Errorf("list organization users: %w", err)
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": "1", "organization": resolvedOrg, "users": page.Items,
				"count": len(page.Items), "nextPageToken": page.NextPageToken,
			})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().IntVar(&limit, "limit", 1000, "Maximum users to return (1-1000)")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgConnectionsCmd() *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use: "connections", Short: "List ERP and exchange connections with sync state", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			connections, err := client.ConnectionDetails(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("list organization connections: %w", err)
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "connections": connections, "count": len(connections)})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgTokensCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "tokens", Short: "List and analyze tokens present in the organization"}
	cmd.AddCommand(newOrgTokensListCmd(), newOrgSpamTokensCmd())
	return cmd
}

func newOrgTokensListCmd() *cobra.Command {
	var orgID, query string
	var limit int
	cmd := &cobra.Command{
		Use: "list", Aliases: []string{"all"}, Short: "List organization tokens, including spam tokens", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > 10000 {
				return errors.New("--limit must be between 1 and 10000")
			}
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			tokens, err := client.OrganizationTokens(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("list organization tokens: %w", err)
			}
			sort.Strings(tokens)
			query = strings.ToLower(strings.TrimSpace(query))
			filtered := make([]string, 0, len(tokens))
			for _, token := range tokens {
				if query == "" || strings.Contains(strings.ToLower(token), query) {
					filtered = append(filtered, token)
				}
			}
			total := len(filtered)
			if len(filtered) > limit {
				filtered = filtered[:limit]
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "tokens": filtered, "total": total, "truncated": total > len(filtered)})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().StringVar(&query, "query", "", "Case-insensitive token substring")
	cmd.Flags().IntVar(&limit, "limit", 1000, "Maximum tokens to return")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

type spamTokenRow struct {
	Token           string `json:"token"`
	Name            string `json:"name,omitempty"`
	ContractAddress string `json:"contractAddress,omitempty"`
	CoinID          string `json:"coinId,omitempty"`
	NetworkID       string `json:"networkId,omitempty"`
	SpamScore       string `json:"spamScore,omitempty"`
	SpamReason      string `json:"spamReason"`
}

type spamScanResult struct {
	Rows  []spamTokenRow
	Error string
}

func newOrgSpamTokensCmd() *cobra.Command {
	var f transactionMutationFlags
	var limit, concurrency int
	var threshold float64
	var createRules bool
	cmd := &cobra.Command{
		Use: "spam", Short: "Score organization tokens for spam and optionally create ignore rules", Args: cobra.NoArgs,
		Long: `Look up metadata for the organization's tokens and flag tokens whose service
spam score meets --threshold or whose symbol/name matches common airdrop,
claim-link, social-bait, or obfuscation patterns.

Scanning is read-only. --create-ignore-rules adds one disabled=false Ignore rule
per detected token and requires --yes; use --dry-run to preview those rules.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > 10000 {
				return mutationError(cmd, "scan-spam-tokens", f.jsonOutput, errors.New("--limit must be between 1 and 10000"))
			}
			if concurrency < 1 || concurrency > 25 {
				return mutationError(cmd, "scan-spam-tokens", f.jsonOutput, errors.New("--concurrency must be between 1 and 25"))
			}
			if threshold < 0 || threshold > 1 {
				return mutationError(cmd, "scan-spam-tokens", f.jsonOutput, errors.New("--threshold must be between 0 and 1"))
			}
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, "scan-spam-tokens", f.jsonOutput, err)
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			tokens, err := client.OrganizationTokens(cmd.Context(), orgID)
			if err != nil {
				return mutationError(cmd, "scan-spam-tokens", f.jsonOutput, fmt.Errorf("list organization tokens: %w", err))
			}
			sort.Strings(tokens)
			truncated := len(tokens) > limit
			if truncated {
				tokens = tokens[:limit]
			}
			results := make([]spamScanResult, len(tokens))
			jobs := make(chan int)
			var workers sync.WaitGroup
			for range min(concurrency, len(tokens)) {
				workers.Add(1)
				go func() {
					defer workers.Done()
					for index := range jobs {
						metadata, lookupErr := client.PublicSymbol(cmd.Context(), tokens[index])
						if lookupErr != nil {
							results[index].Error = lookupErr.Error()
							continue
						}
						results[index].Rows = spamRows(tokens[index], metadata, threshold)
					}
				}()
			}
			for index := range tokens {
				jobs <- index
			}
			close(jobs)
			workers.Wait()

			rows := make([]spamTokenRow, 0)
			lookupErrors := make([]map[string]string, 0)
			spamCoins := make([]string, 0)
			for index, result := range results {
				rows = append(rows, result.Rows...)
				if len(result.Rows) > 0 {
					spamCoins = append(spamCoins, result.Rows[0].Token)
				}
				if result.Error != "" {
					lookupErrors = append(lookupErrors, map[string]string{"token": tokens[index], "error": result.Error})
				}
			}
			spamCoins = uniqueNonEmpty(spamCoins)
			result := map[string]any{"tokensScanned": len(tokens), "spamTokenCount": len(spamCoins), "rows": rows, "lookupErrors": lookupErrors, "truncated": truncated, "threshold": threshold}
			if !createRules {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": orgID, "result": result})
			}
			rules := spamIgnoreRules(spamCoins)
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: "create-spam-ignore-rules", Organization: orgID, DryRun: true, Request: rules, Result: result})
			}
			if !f.yes {
				return mutationError(cmd, "create-spam-ignore-rules", f.jsonOutput, errors.New("refusing to create ignore rules without --yes (use --dry-run to preview)"))
			}
			created, failed := 0, make([]map[string]string, 0)
			for index, rule := range rules {
				body, _ := json.Marshal(rule)
				if _, createErr := client.CreateRule(cmd.Context(), orgID, body); createErr != nil {
					failed = append(failed, map[string]string{"token": spamCoins[index], "error": createErr.Error()})
				} else {
					created++
				}
			}
			result["rulesCreated"] = created
			result["ruleFailures"] = failed
			status := "success"
			if len(failed) > 0 {
				status = "partial_failure"
			}
			envelope := mutationEnvelope{SchemaVersion: "1", Status: status, Operation: "create-spam-ignore-rules", Organization: orgID, Result: result}
			if len(failed) > 0 {
				_ = writeJSON(cmd.OutOrStdout(), envelope)
				return fmt.Errorf("created %d of %d spam ignore rules", created, len(rules))
			}
			return outputMutation(cmd, f.jsonOutput, envelope, fmt.Sprintf("created %d spam ignore rule(s)\n", created))
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().IntVar(&limit, "limit", 500, "Maximum organization tokens to inspect")
	cmd.Flags().IntVar(&concurrency, "concurrency", 8, "Concurrent metadata lookups (1-25)")
	cmd.Flags().Float64Var(&threshold, "threshold", 0.5, "Service spam-score threshold")
	cmd.Flags().BoolVar(&createRules, "create-ignore-rules", false, "Create one Ignore rule per detected token")
	return cmd
}

func spamIgnoreRules(coins []string) []map[string]any {
	rules := make([]map[string]any, 0, len(coins))
	for _, coin := range coins {
		rules = append(rules, map[string]any{"transfer": map[string]any{
			"accountingConnectionId": "Manual", "action": map[string]string{"type": "Ignore"},
			"disabled": false, "name": "ignore spam " + coin, "priority": 1, "coin": coin,
			"direction": "All", "allowMismatch": false, "autoCategorizeFee": false,
			"collapseValues": false, "multiToken": false,
		}})
	}
	return rules
}

func spamRows(fallback string, raw json.RawMessage, threshold float64) []spamTokenRow {
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}
	metadata := root
	if item, ok := root["item"].(map[string]any); ok {
		metadata = item
	}
	if meta, ok := metadata["meta"].(map[string]any); ok {
		metadata = meta
	} else if meta, ok := root["meta"].(map[string]any); ok {
		metadata = meta
	}
	symbol := firstString(metadata["symbol"], root["symbol"], fallback)
	name := firstString(metadata["name"], root["name"])
	score, scoreText := spamScore(metadata["spamScore"], root["spamScore"])
	reason := spamTextReason(symbol, name)
	if score >= threshold {
		reason = "spam_score"
	}
	if reason == "" {
		return nil
	}
	coinID := firstString(metadata["coinId"], root["coinId"], metadata["id"], root["id"])
	rows := make([]spamTokenRow, 0)
	if addresses, ok := metadata["addresses"].([]any); ok {
		for _, entry := range addresses {
			address, _ := entry.(map[string]any)
			rows = append(rows, spamTokenRow{Token: symbol, Name: name, CoinID: coinID, SpamScore: scoreText, SpamReason: reason, NetworkID: firstString(address["networkId"], address["network"]), ContractAddress: firstString(address["address"], address["contractAddress"])})
		}
	}
	if len(rows) == 0 {
		rows = append(rows, spamTokenRow{Token: symbol, Name: name, CoinID: coinID, SpamScore: scoreText, SpamReason: reason, NetworkID: firstString(metadata["networkId"], root["networkId"]), ContractAddress: firstString(metadata["contractAddress"], root["contractAddress"], metadata["address"], root["address"])})
	}
	return rows
}

func firstString(values ...any) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func spamScore(values ...any) (float64, string) {
	text := firstString(values...)
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return -1, text
	}
	return value, text
}

var cyrillicPattern = regexp.MustCompile(`[\p{Cyrillic}]`)

func spamTextReason(values ...string) string {
	text := strings.ToLower(strings.Join(values, " "))
	patterns := []struct {
		reason string
		terms  []string
	}{
		{"claim_language", []string{"claim", "airdrop", "reward now", "free token"}},
		{"url", []string{"http://", "https://", ".com", ".io/", "www."}},
		{"action_language", []string{"visit", "click", "redeem", "verify wallet"}},
		{"social_bait", []string{"telegram", "discord", "whatsapp"}},
		{"mojibake", []string{"�", "Ã", "Â"}},
	}
	for _, pattern := range patterns {
		for _, term := range pattern.terms {
			if strings.Contains(text, strings.ToLower(term)) {
				return pattern.reason
			}
		}
	}
	if cyrillicPattern.MatchString(text) {
		return "cyrillic_or_mixed_script"
	}
	if strings.Contains(text, "....") {
		return "excessive_periods"
	}
	return ""
}
