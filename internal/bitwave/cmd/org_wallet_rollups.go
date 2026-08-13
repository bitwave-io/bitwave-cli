package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func newOrgWalletsRollupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollup",
		Short: "Inspect or configure modern Babel wallet rollups",
		Long:  "Manage new-babel-rollups attached to an organization wallet. These commands do not use the legacy wallet rollupConfig.",
	}
	cmd.AddCommand(newOrgWalletRollupGetCmd(), newOrgWalletRollupSetCmd())
	return cmd
}

func newOrgWalletRollupGetCmd() *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use:   "get WALLET_ID_OR_NAME",
		Short: "Get the deployed Babel rollup configuration for a wallet",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			wallet, err := resolveOrganizationWallet(cmd, client, resolvedOrg, args[0])
			if err != nil {
				return err
			}
			rollup, err := client.WalletRollup(cmd.Context(), resolvedOrg, wallet.ID)
			if err != nil {
				return fmt.Errorf("get wallet rollup: %w", err)
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "wallet": wallet, "rollup": rollup})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgWalletRollupSetCmd() *cobra.Command {
	var f transactionMutationFlags
	var input, address string
	cmd := &cobra.Command{
		Use:   "set WALLET_ID_OR_NAME",
		Short: "Create or replace a wallet's Babel rollup rules",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			operation := "set-wallet-babel-rollup"
			rules, err := readBabelRollupRules(input)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			for i, rule := range rules {
				if err := validateBabelRollupRule(rule); err != nil {
					return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("rule %d: %w", i+1, err))
				}
			}
			resolvedOrg, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			wallet, err := resolveOrganizationWallet(cmd, client, resolvedOrg, args[0])
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			resolvedAddress := strings.TrimSpace(address)
			if resolvedAddress == "" {
				resolvedAddress = wallet.Address
			}
			if resolvedAddress == "" && len(wallet.Addresses) > 0 {
				resolvedAddress = wallet.Addresses[0]
			}
			if resolvedAddress == "" {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("wallet address was not returned; provide --address"))
			}
			preview := map[string]any{"method": "POST", "path": fmt.Sprintf("/orgs/%s/wallets/%s/rollup", resolvedOrg, wallet.ID), "body": orgreports.WalletRollupRequest{Address: resolvedAddress, Type: "rollup-by-time", Rules: rules}}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: resolvedOrg, DryRun: true, Request: preview})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to change the organization without --yes (use --dry-run to preview)"))
			}
			if err := client.UpsertWalletRollup(cmd.Context(), resolvedOrg, wallet.ID, resolvedAddress, rules); err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("set wallet rollup: %w", err))
			}
			return outputMutation(cmd, f.jsonOutput, mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: resolvedOrg, Result: map[string]any{"wallet": wallet, "ruleCount": len(rules), "type": "rollup-by-time"}}, fmt.Sprintf("configured %d Babel rollup rule(s) for %s\n", len(rules), wallet.Name))
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringVarP(&input, "input", "i", "", "Babel rollup rules JSON file (required)")
	cmd.Flags().StringVar(&address, "address", "", "Wallet address override")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}

func readBabelRollupRules(path string) ([]orgreports.BabelRollupRule, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("Babel rollup rules input is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Babel rollup rules: %w", err)
	}
	var rules []orgreports.BabelRollupRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("decode Babel rollup rules: %w", err)
	}
	if len(rules) == 0 {
		return nil, errors.New("at least one Babel rollup rule is required")
	}
	return rules, nil
}

func resolveOrganizationWallet(cmd *cobra.Command, client *orgreports.Client, orgID, value string) (*orgreports.Wallet, error) {
	wallets, err := client.Wallets(cmd.Context(), orgID)
	if err != nil {
		return nil, fmt.Errorf("list organization wallets: %w", err)
	}
	var match *orgreports.Wallet
	for i := range wallets {
		if wallets[i].ID == value || strings.EqualFold(wallets[i].Name, value) {
			if match != nil && wallets[i].ID != value {
				return nil, fmt.Errorf("wallet name %q is ambiguous; use wallet ID", value)
			}
			candidate := wallets[i]
			match = &candidate
			if wallets[i].ID == value {
				break
			}
		}
	}
	if match == nil {
		return nil, fmt.Errorf("wallet %q was not found", value)
	}
	return match, nil
}
