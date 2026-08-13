package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func newOrgAccountingStarterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "starter",
		Short: "Inspect or apply the conservative Bitwave starter categories and contacts",
		Long: `The starter set contains only general revenue, general expense, and gas-fee
fallbacks. On the implicit Manual connection it reuses the built-in Digital
Assets and Crypto Fees accounts and never creates a generated manual connection.
Every more-specific account or contact must be supplied or approved by the user.`,
	}
	cmd.AddCommand(newOrgAccountingStarterShowCmd(), newOrgAccountingStarterApplyCmd())
	return cmd
}

func newOrgAccountingStarterShowCmd() *cobra.Command {
	var connectionID string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Return the starter set and LLM guardrails without changing the organization",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "starter": starterPolicy(strings.TrimSpace(connectionID))})
		},
	}
	cmd.Flags().StringVar(&connectionID, "accounting-connection", "", "Accounting connection ID to place in the proposed records")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgAccountingStarterApplyCmd() *cobra.Command {
	var f transactionMutationFlags
	var connectionID string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create the idempotent conservative starter categories and contacts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			operation := "apply-accounting-starter"
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			_, client, err := accountingClient(orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			resolvedConnection, err := resolveStarterConnection(cmd, client, orgID, connectionID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			policy := starterPolicy(resolvedConnection)
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: policy})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to change the organization without --yes (use --dry-run to preview)"))
			}
			result, err := applyStarter(cmd, client, orgID, policy)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: result})
		},
	}
	cmd.Flags().StringVar(&f.orgID, "org", "", "Organization ID override")
	cmd.Flags().StringVar(&connectionID, "accounting-connection", "", "Accounting connection ID; inferred only when exactly one active connection exists")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "Preview the starter records without writing")
	cmd.Flags().BoolVar(&f.yes, "yes", false, "Confirm creation of missing starter records")
	cmd.Flags().BoolVar(&f.jsonOutput, "json", true, "Emit machine-readable JSON")
	return cmd
}

func resolveStarterConnection(cmd *cobra.Command, client *orgreports.Client, orgID, requested string) (string, error) {
	if id := strings.TrimSpace(requested); id != "" {
		return id, nil
	}
	connections, err := client.AccountingConnections(cmd.Context(), orgID)
	if err != nil {
		return "", fmt.Errorf("list accounting connections: %w", err)
	}
	connections = withImplicitManualConnection(connections)
	active := make([]string, 0, len(connections))
	for _, connection := range connections {
		if !connection.Disabled {
			active = append(active, connection.ID)
		}
	}
	for _, id := range active {
		if strings.EqualFold(id, implicitManualConnectionID) {
			return implicitManualConnectionID, nil
		}
	}
	if len(active) != 1 {
		return "", fmt.Errorf("--accounting-connection is required when the organization has %d active accounting connections", len(active))
	}
	return active[0], nil
}

func applyStarter(cmd *cobra.Command, client *orgreports.Client, orgID string, policy accountingStarterPolicy) (map[string]any, error) {
	categories, err := client.Categories(cmd.Context(), orgID)
	if err != nil {
		return nil, fmt.Errorf("list existing categories: %w", err)
	}
	contacts, err := client.Contacts(cmd.Context(), orgID)
	if err != nil {
		return nil, fmt.Errorf("list existing contacts: %w", err)
	}
	existingCategories := map[string]bool{}
	for _, item := range categories {
		if item.Enabled && strings.EqualFold(strings.TrimSpace(item.AccountingConnectionID), strings.TrimSpace(policy.Categories[0].ConnectionID)) {
			existingCategories[strings.ToLower(strings.TrimSpace(item.Name))] = true
		}
	}
	existingContacts := map[string]bool{}
	for _, item := range contacts {
		if item.Enabled && strings.EqualFold(strings.TrimSpace(item.AccountingConnectionID), strings.TrimSpace(policy.Contacts[0].ConnectionID)) {
			existingContacts[strings.ToLower(strings.TrimSpace(item.Name))] = true
		}
	}
	createdCategories, skippedCategories := []string{}, []string{}
	for _, item := range policy.Categories {
		if existingCategories[strings.ToLower(item.Name)] {
			skippedCategories = append(skippedCategories, item.Name)
			continue
		}
		if _, err := createChartAccountWithRetry(cmd, client, orgID, accountRequest(item)); err != nil {
			return nil, fmt.Errorf("create starter category %q: %w", item.Name, err)
		}
		createdCategories = append(createdCategories, item.Name)
	}
	createdContacts, skippedContacts := []string{}, []string{}
	for _, item := range policy.Contacts {
		if existingContacts[strings.ToLower(item.Name)] {
			skippedContacts = append(skippedContacts, item.Name)
			continue
		}
		if _, err := client.CreateContact(cmd.Context(), orgID, item); err != nil {
			return nil, fmt.Errorf("create starter contact %q: %w", item.Name, err)
		}
		createdContacts = append(createdContacts, item.Name)
	}
	return map[string]any{
		"accountingConnectionId": policy.Categories[0].ConnectionID,
		"createdCategories":      createdCategories,
		"skippedCategories":      skippedCategories,
		"createdContacts":        createdContacts,
		"skippedContacts":        skippedContacts,
		"guardrails":             policy.Guardrails,
		"nextCommand":            "bitwave org accounting status --json",
	}, nil
}
