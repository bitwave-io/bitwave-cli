package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

type ruleSummary struct {
	ID                     string          `json:"id"`
	Name                   string          `json:"name"`
	Disabled               bool            `json:"disabled"`
	Type                   string          `json:"type,omitempty"`
	Priority               int             `json:"priority,omitempty"`
	Coin                   string          `json:"coin,omitempty"`
	MethodID               string          `json:"methodId,omitempty"`
	WalletID               string          `json:"walletId,omitempty"`
	Direction              string          `json:"direction,omitempty"`
	FromAddress            string          `json:"fromAddress,omitempty"`
	ToAddress              string          `json:"toAddress,omitempty"`
	AccountingConnectionID string          `json:"accountingConnectionId,omitempty"`
	AfterDateSEC           json.RawMessage `json:"afterDateSEC,omitempty"`
	BeforeDateSEC          json.RawMessage `json:"beforeDateSEC,omitempty"`
	Action                 json.RawMessage `json:"action,omitempty"`
	MetadataRule           json.RawMessage `json:"metadataRule,omitempty"`
}

func newOrgRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "rule",
		Aliases: []string{"rules"},
		Short:   "Discover, create, and validate Bitwave categorization rules",
		Long: `Manage organization categorization rules through Bitwave's rule API.

Rule creation changes future organization behavior. Every create requires
--yes, supports --dry-run, and should be validated against a known transaction
before an enabled rule is run across historical data.`,
	}
	cmd.AddCommand(
		newListRulesCmd(), newGetRuleCmd(), newCreateRawRuleCmd(), newValidateRuleCmd(),
		newToggleRuleCmd("enable", false), newToggleRuleCmd("disable", true), newDeleteRuleCmd(),
		newRunRulesCmd(),
		newRuleRecipesCmd(), newRuleMetadataGuideCmd(), newRuleContextCmd(), newRulePlanCmd(), newRuleApplyCmd(), newRuleFlowsCmd(),
	)
	return cmd
}

func newRunRulesCmd() *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Trigger organization categorization rules now instead of waiting for the background schedule",
		Long: `Trigger an asynchronous organization-wide rules run.

Bitwave normally runs rules in the background roughly twice per day. Creating
or enabling a rule does not apply it immediately. Use this command after rule
setup when the user wants processing to begin sooner.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			operation := "run-rules"
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			preview := map[string]any{"operation": "RunRulesForOrg", "variables": map[string]any{"orgId": orgID}}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: preview})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to trigger an organization-wide rules run without --yes (use --dry-run to preview)"))
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			if err := client.RunRules(cmd.Context(), orgID); err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: map[string]any{"triggered": true, "asynchronous": true}})
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}

func newGetRuleCmd() *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use:   "get RULE_ID",
		Short: "Get one complete organization rule without listing every rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			rule, err := client.Rule(cmd.Context(), resolvedOrg, args[0])
			if err != nil {
				return fmt.Errorf("get rule %s: %w", args[0], err)
			}
			_, err = cmd.OutOrStdout().Write(append(rule, '\n'))
			return err
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newToggleRuleCmd(name string, disabled bool) *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{
		Use:   name + " RULE_ID",
		Short: strings.ToUpper(name[:1]) + name[1:] + " an organization categorization rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, name+"-rule", f.jsonOutput, err)
			}
			preview := map[string]any{"operation": "ToggleRuleStatus", "variables": map[string]any{"orgId": orgID, "ruleId": args[0], "disabled": disabled}}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: name + "-rule", Organization: orgID, DryRun: true, Request: preview})
			}
			if !f.yes {
				return mutationError(cmd, name+"-rule", f.jsonOutput, errors.New("refusing to change a rule without --yes (use --dry-run to preview)"))
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			if err := client.ToggleRule(cmd.Context(), orgID, args[0], disabled); err != nil {
				return mutationError(cmd, name+"-rule", f.jsonOutput, err)
			}
			return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: name + "-rule", Organization: orgID, Result: map[string]any{"ruleId": args[0], "disabled": disabled}})
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}

func newDeleteRuleCmd() *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{
		Use:   "delete RULE_ID",
		Short: "Permanently delete an organization categorization rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, "delete-rule", f.jsonOutput, err)
			}
			preview := map[string]any{"operation": "DeleteRule", "variables": map[string]any{"orgId": orgID, "ruleId": args[0]}}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: "delete-rule", Organization: orgID, DryRun: true, Request: preview})
			}
			if !f.yes {
				return mutationError(cmd, "delete-rule", f.jsonOutput, errors.New("refusing to permanently delete a rule without --yes (use --dry-run to preview)"))
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			if err := client.DeleteRule(cmd.Context(), orgID, args[0]); err != nil {
				return mutationError(cmd, "delete-rule", f.jsonOutput, err)
			}
			return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: "delete-rule", Organization: orgID, Result: map[string]any{"ruleId": args[0], "deleted": true}})
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}

func newListRulesCmd() *cobra.Command {
	var orgID, query string
	var limit int
	var includeDisabled, full bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a bounded set of organization categorization rules",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > 500 {
				return errors.New("--limit must be between 1 and 500")
			}
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			originalQuery := strings.TrimSpace(query)
			rawRules := []json.RawMessage{}
			totalKnown := true
			moreAvailable := false
			if originalQuery == "" {
				totalKnown = false
				token := ""
				for {
					page, pageErr := client.RulesPage(cmd.Context(), resolvedOrg, min(limit*2, 500), token)
					if pageErr != nil {
						return fmt.Errorf("list rules: %w", pageErr)
					}
					rawRules = append(rawRules, page.Items...)
					matching := 0
					for _, raw := range rawRules {
						var summary ruleSummary
						if json.Unmarshal(raw, &summary) == nil && (includeDisabled || !summary.Disabled) {
							matching++
						}
					}
					if matching >= limit || page.NextPageToken == "" || page.NextPageToken == token {
						moreAvailable = page.NextPageToken != ""
						break
					}
					token = page.NextPageToken
				}
			} else {
				var loadErr error
				rawRules, loadErr = client.Rules(cmd.Context(), resolvedOrg)
				if loadErr != nil {
					return fmt.Errorf("list rules: %w", loadErr)
				}
			}
			query = strings.ToLower(strings.TrimSpace(query))
			matchingRaw := make([]json.RawMessage, 0)
			matchingSummary := make([]ruleSummary, 0)
			for _, raw := range rawRules {
				var summary ruleSummary
				if json.Unmarshal(raw, &summary) != nil {
					continue
				}
				if !includeDisabled && summary.Disabled {
					continue
				}
				haystack := strings.ToLower(strings.Join([]string{summary.ID, summary.Name, summary.Type, summary.Coin, summary.MethodID, summary.WalletID, summary.Direction, summary.FromAddress, summary.ToAddress, summary.AccountingConnectionID, string(summary.Action), string(summary.MetadataRule)}, " "))
				if query != "" && !strings.Contains(haystack, query) {
					continue
				}
				matchingRaw = append(matchingRaw, raw)
				matchingSummary = append(matchingSummary, summary)
				if len(matchingSummary) == limit {
					break
				}
			}
			var rules any = matchingSummary
			if full {
				rules = matchingRaw
			}
			warnings := []string{}
			if len(matchingSummary) == limit && (len(rawRules) > limit || moreAvailable) {
				warnings = append(warnings, "Rule results may be truncated; narrow --query or increase --limit.")
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": "1", "organization": resolvedOrg, "totalRules": map[bool]any{true: len(rawRules), false: nil}[totalKnown], "totalRulesKnown": totalKnown,
				"count": len(matchingSummary), "rules": rules, "resultShape": map[bool]string{true: "full", false: "compact"}[full],
				"filters": map[string]any{"query": query, "includeDisabled": includeDisabled, "limit": limit}, "warnings": warnings,
			})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().StringVar(&query, "query", "", "Case-insensitive rule name, ID, asset, method ID, metadata, wallet, address, direction, or action substring")
	cmd.Flags().IntVar(&limit, "limit", 25, "Maximum rules to return (1-500)")
	cmd.Flags().BoolVar(&includeDisabled, "include-disabled", false, "Include disabled rules")
	cmd.Flags().BoolVar(&full, "full", false, "Return complete rule objects")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newCreateRawRuleCmd() *cobra.Command {
	var f transactionMutationFlags
	var input string
	var enabled bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a rule from the complete Bitwave Rule input contract",
		RunE: func(cmd *cobra.Command, _ []string) error {
			operation := "create-rule"
			rule, err := readRuleObject(input, cmd.InOrStdin())
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			rule, err = prepareRuleObject(rule, enabled)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			preview := map[string]any{"method": "POST", "service": "rules GraphQL", "operation": "CreateRule", "variables": map[string]any{"orgId": orgID, "rule": rule}}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: preview})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to create an organization rule without --yes (use --dry-run to preview)"))
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			result, err := client.CreateRule(cmd.Context(), orgID, rule)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			return outputMutation(cmd, f.jsonOutput, mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: result}, "created organization rule\n")
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringVarP(&input, "input", "i", "", "Complete Rule JSON file, or - for stdin (required)")
	cmd.Flags().BoolVar(&enabled, "enabled", false, "Permit creation of an enabled recurring rule (rules are forced disabled by default)")
	return cmd
}

func newValidateRuleCmd() *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use:   "validate RULE_ID TRANSACTION_ID",
		Short: "Validate whether one rule applies to one transaction",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			result, err := client.ValidateRule(cmd.Context(), resolvedOrg, args[1], args[0])
			if err != nil {
				return fmt.Errorf("validate rule %s on transaction %s: %w", args[0], args[1], err)
			}
			_, err = cmd.OutOrStdout().Write(append(result, '\n'))
			return err
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func readRuleObject(path string, stdin io.Reader) (json.RawMessage, error) {
	if path == "" {
		return nil, errors.New("--input is required")
	}
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(io.LimitReader(stdin, 4<<20))
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read rule input: %w", err)
	}
	var object map[string]any
	if len(data) == 0 || json.Unmarshal(data, &object) != nil || object == nil {
		return nil, errors.New("rule input must be a JSON object")
	}
	if len(object) != 1 {
		return nil, errors.New("Rule input must contain exactly one top-level rule variant, such as `transfer`")
	}
	return json.RawMessage(data), nil
}

func prepareRuleObject(rule json.RawMessage, allowEnabled bool) (json.RawMessage, error) {
	var root map[string]any
	if err := json.Unmarshal(rule, &root); err != nil {
		return nil, errors.New("rule input must be a JSON object")
	}
	for variant, raw := range root {
		object, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("rule variant %q must be a JSON object", variant)
		}
		if name, _ := object["name"].(string); strings.TrimSpace(name) == "" {
			return nil, errors.New("rule input requires a non-empty `name`")
		}
		if object["action"] == nil {
			return nil, errors.New("rule input requires `action`")
		}
		if connection, _ := object["accountingConnectionId"].(string); strings.TrimSpace(connection) == "" {
			return nil, errors.New("rule input requires `accountingConnectionId`")
		}
		if _, ok := object["priority"].(float64); !ok {
			return nil, errors.New("rule input requires numeric `priority`")
		}
		disabled, present := object["disabled"].(bool)
		if !allowEnabled {
			if present && !disabled {
				return nil, errors.New("input contains an enabled rule; pass --enabled explicitly or set `disabled` to true")
			}
			object["disabled"] = true
		}
		root[variant] = object
	}
	prepared, err := json.Marshal(root)
	return json.RawMessage(prepared), err
}
