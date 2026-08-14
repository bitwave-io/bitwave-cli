package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func newOrgInventoryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "inventory", Short: "Manage Bitwave organization inventory views"}
	cmd.AddCommand(newOrgInventoryListCmd(), newOrgInventoryCreateCmd(), newOrgInventoryUpdateCmd(), newOrgInventoryUpdatesCmd(), newOrgInventoryDeleteCmd())
	return cmd
}

func newOrgInventoryListCmd() *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use: "list", Short: "List organization inventory views",
		RunE: func(cmd *cobra.Command, _ []string) error {
			orgID, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			views, err := client.InventoryViews(cmd.Context(), orgID)
			if err != nil {
				return fmt.Errorf("list inventory views: %w", err)
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": orgID, "inventoryViews": views})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgInventoryCreateCmd() *cobra.Command {
	var f transactionMutationFlags
	var input string
	cmd := &cobra.Command{
		Use: "create", Short: "Create an inventory view from a complete Bitwave JSON request",
		RunE: func(cmd *cobra.Command, _ []string) error {
			operation := "create-inventory-view"
			request, err := readJSONObject(input, cmd.InOrStdin())
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: request})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to create an inventory view without --yes"))
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			result, err := client.CreateInventoryView(cmd.Context(), orgID, request)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: result})
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringVarP(&input, "input", "i", "", "Complete inventory-view JSON request, or - for stdin (required)")
	return cmd
}

func newOrgInventoryUpdateCmd() *cobra.Command {
	var f transactionMutationFlags
	var asOf, referenceRun, referenceEndDate string
	var historical bool
	cmd := &cobra.Command{
		Use: "update VIEW_ID_OR_NAME", Short: "Start an inventory-view calculation", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			operation := "update-inventory-view"
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			org, err := client.Org(cmd.Context(), orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			asOf, err = resolveInventoryUpdateDate(asOf, org.Timezone, time.Now())
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			views, err := client.InventoryViews(cmd.Context(), orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			view, err := resolveInventoryView(args[0], views)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			if (referenceRun == "") != (referenceEndDate == "") {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("--reference-run and --reference-end-date must be supplied together"))
			}
			request := orgreports.InventoryViewUpdateRequest{RunIDReference: referenceRun, StartingDate: referenceEndDate, EndingDate: asOf, TransferAtHistoricalCost: historical}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: request})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to start an inventory calculation without --yes"))
			}
			result, err := client.TriggerInventoryViewUpdate(cmd.Context(), orgID, view.ID, request)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: result})
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringVar(&asOf, "as-of", "", "Run end date in YYYY-MM-DD (default: yesterday in the organization timezone)")
	cmd.Flags().StringVar(&referenceRun, "reference-run", "", "Optional prior inventory update ID")
	cmd.Flags().StringVar(&referenceEndDate, "reference-end-date", "", "Prior run end date in YYYY-MM-DD")
	cmd.Flags().BoolVar(&historical, "transfer-at-historical-cost", false, "Set transferAtHistoricalCost on the update request")
	return cmd
}

func newOrgInventoryUpdatesCmd() *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use: "updates VIEW_ID_OR_NAME", Short: "List inventory calculation runs and errors", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			orgID, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			views, err := client.InventoryViews(cmd.Context(), orgID)
			if err != nil {
				return err
			}
			view, err := resolveInventoryView(args[0], views)
			if err != nil {
				return err
			}
			updates, err := client.InventoryViewUpdates(cmd.Context(), orgID, view.ID)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": orgID, "inventoryView": view, "updates": updates})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgInventoryDeleteCmd() *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{
		Use: "delete VIEW_ID_OR_NAME", Short: "Delete an inventory view", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			operation := "delete-inventory-view"
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			views, err := client.InventoryViews(cmd.Context(), orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			view, err := resolveInventoryView(args[0], views)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: view})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to delete an inventory view without --yes"))
			}
			if err := client.DeleteInventoryView(cmd.Context(), orgID, view.ID); err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: view})
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}

func resolveInventoryUpdateDate(requested, timezone string, now time.Time) (string, error) {
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return "", fmt.Errorf("organization timezone %q is invalid: %w", timezone, err)
	}
	maximum := now.In(location).AddDate(0, 0, -1).Format("2006-01-02")
	if strings.TrimSpace(requested) == "" {
		return maximum, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", requested, location)
	if err != nil {
		return "", errors.New("date must use YYYY-MM-DD")
	}
	maxDate, _ := time.ParseInLocation("2006-01-02", maximum, location)
	if parsed.After(maxDate) {
		return "", fmt.Errorf("latest allowed date is %s", maximum)
	}
	return requested, nil
}
