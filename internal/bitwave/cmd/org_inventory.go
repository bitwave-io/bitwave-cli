package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

const (
	usGAAPProfile           = "us-gaap"
	usFederalTaxFIFOProfile = "us-federal-tax-fifo"
)

type inventoryGuidanceSource struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type inventoryProfile struct {
	ID            string                                `json:"id"`
	Jurisdiction  string                                `json:"jurisdiction"`
	Purpose       string                                `json:"purpose"`
	Name          string                                `json:"defaultName"`
	Summary       string                                `json:"summary"`
	Request       orgreports.InventoryViewCreateRequest `json:"request"`
	Confirmations []string                              `json:"confirmations"`
	Limitations   []string                              `json:"limitations"`
	Sources       []inventoryGuidanceSource             `json:"sources"`
	LastReviewed  string                                `json:"lastReviewed"`
	AdvisoryOnly  bool                                  `json:"advisoryOnly"`
}

func newOrgInventoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inventory",
		Short: "Configure and update Bitwave inventory views",
		Long: `Configure the inventory views used for cost basis, gain/loss, and
valuation reporting in a Bitwave organization.

Jurisdiction profiles are starting prompts, not legal, tax, accounting, or
financial advice. Laws and standards change. The user, their LLM, and their
qualified accountant must verify the current primary sources, entity facts,
asset scope, state/local requirements, and intended reporting purpose before
relying on a view. Guidance never blocks an explicit user choice.`,
	}
	cmd.AddCommand(newOrgInventoryListCmd(), newOrgInventoryGuidanceCmd(), newOrgInventoryCreateCmd(), newOrgInventoryUpdateCmd(), newOrgInventoryDeleteCmd())
	return cmd
}

func newOrgInventoryListCmd() *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List organization inventory views",
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			views, err := client.InventoryViews(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("list inventory views: %w", err)
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "inventoryViews": views})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgInventoryGuidanceCmd() *cobra.Command {
	var jurisdiction, purpose string
	cmd := &cobra.Command{
		Use:   "guidance",
		Short: "Return jurisdiction-aware inventory setup prompts for an LLM",
		RunE: func(cmd *cobra.Command, _ []string) error {
			jurisdiction = strings.ToUpper(strings.TrimSpace(jurisdiction))
			purpose = strings.ToLower(strings.TrimSpace(purpose))
			if jurisdiction != "US" {
				return fmt.Errorf("unsupported jurisdiction %q; only US guidance is currently reviewed", jurisdiction)
			}
			profiles := usInventoryProfiles()
			if purpose != "" {
				filtered := profiles[:0]
				for _, profile := range profiles {
					if profile.Purpose == purpose {
						filtered = append(filtered, profile)
					}
				}
				if len(filtered) == 0 {
					return errors.New("--purpose must be books or tax")
				}
				profiles = filtered
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": "1",
				"jurisdiction":  jurisdiction,
				"disclaimer":    "Informational guidance only; not legal, tax, accounting, or financial advice. Verify current primary sources and obtain qualified professional approval.",
				"llmPrompt": []string{
					"Ask whether the view is for financial statements, federal tax, management reporting, or reconciliation; do not infer one from the user's location.",
					"Ask for entity type, fiscal year, accounting framework, industry-specific guidance, state/local filing jurisdictions, and whether the entity issues GAAP financial statements.",
					"Ask the accountant to approve asset scope, lot-selection method, wallet/account mapping, fee treatment, wrapping and internal-transfer treatment, pricing source, and effective date.",
					"For tax specific identification, confirm that contemporaneous identification and substantiating records exist; otherwise surface FIFO as the federal default, not as personalized advice.",
					"Re-check every source at execution time because laws, regulations, standards, and Bitwave capabilities change.",
				},
				"profiles": profiles,
			})
		},
	}
	cmd.Flags().StringVar(&jurisdiction, "jurisdiction", "US", "Reporting jurisdiction (currently: US)")
	cmd.Flags().StringVar(&purpose, "purpose", "", "Optional purpose: books or tax")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgInventoryCreateCmd() *cobra.Command {
	var f transactionMutationFlags
	var profileID, name string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an inventory view from a reviewed profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			operation := "create-inventory-view"
			profile, err := inventoryProfileByID(profileID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			if strings.TrimSpace(name) != "" {
				profile.Request.Name = strings.TrimSpace(name)
			}
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			warnings := append([]string{"Guidance only; qualified professional approval remains required."}, profile.Confirmations...)
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: profile.Request, Warnings: warnings})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to change the organization without --yes (use --dry-run to preview)"))
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			views, err := client.InventoryViews(cmd.Context(), orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("check existing inventory views: %w", err))
			}
			for _, view := range views {
				if strings.EqualFold(view.Name, profile.Request.Name) {
					return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: map[string]any{"status": "skipped_existing", "inventoryView": view}, Warnings: warnings})
				}
			}
			result, err := client.CreateInventoryView(cmd.Context(), orgID, profile.Request)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("create inventory view: %w", err))
			}
			return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: map[string]any{"status": "created", "id": result.ID, "name": profile.Request.Name, "profile": profile.ID}, Warnings: warnings})
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringVar(&profileID, "profile", "", "Reviewed profile: us-gaap or us-federal-tax-fifo (required)")
	cmd.Flags().StringVar(&name, "name", "", "Override the profile's default view name")
	_ = cmd.MarkFlagRequired("profile")
	return cmd
}

func newOrgInventoryUpdateCmd() *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{
		Use:   "update VIEW_ID_OR_NAME",
		Short: "Start an inventory-view calculation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			operation := "update-inventory-view"
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			views, err := client.InventoryViews(cmd.Context(), orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("list inventory views: %w", err))
			}
			view, err := resolveInventoryView(args[0], views)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			request := map[string]any{"method": "POST", "path": "/orgs/" + orgID + "/inventory-views/" + view.ID + "/updates", "inventoryView": view}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: request})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to start the inventory calculation without --yes (use --dry-run to preview)"))
			}
			result, err := client.TriggerInventoryViewUpdate(cmd.Context(), orgID, view.ID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("start inventory-view update: %w", err))
			}
			return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: map[string]any{"status": "started", "inventoryView": view, "runId": result.ID}})
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}

func newOrgInventoryDeleteCmd() *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{
		Use:   "delete VIEW_ID_OR_NAME",
		Short: "Permanently delete one inventory view",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			operation := "delete-inventory-view"
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			views, err := client.InventoryViews(cmd.Context(), orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("list inventory views: %w", err))
			}
			view, err := resolveInventoryView(args[0], views)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			request := map[string]any{"method": "DELETE", "path": "/orgs/" + orgID + "/inventory-views/" + view.ID, "inventoryView": view}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: request, Warnings: []string{"Inventory-view deletion is permanent."}})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to permanently delete the inventory view without --yes (use --dry-run to preview)"))
			}
			if err := client.DeleteInventoryView(cmd.Context(), orgID, view.ID); err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("delete inventory view: %w", err))
			}
			return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: map[string]any{"status": "deleted", "inventoryView": view}})
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}

func inventoryProfileByID(id string) (inventoryProfile, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, profile := range usInventoryProfiles() {
		if profile.ID == id {
			return profile, nil
		}
	}
	return inventoryProfile{}, fmt.Errorf("unknown --profile %q; use us-gaap or us-federal-tax-fifo", id)
}

func usInventoryProfiles() []inventoryProfile {
	sources := []inventoryGuidanceSource{
		{Title: "IRS Digital Assets", URL: "https://www.irs.gov/filing/digital-assets"},
		{Title: "IRS Digital Asset Transaction FAQs", URL: "https://www.irs.gov/individuals/international-taxpayers/frequently-asked-questions-on-digital-asset-transactions"},
		{Title: "IRS Revenue Procedure 2024-28", URL: "https://www.irs.gov/pub/irs-drop/rp-24-28.pdf"},
		{Title: "FASB ASU 2023-08", URL: "https://storage.fasb.org/ASU%202023-08.pdf"},
		{Title: "Bitwave Inventory Views", URL: "https://docs.bitwave.io/docs/inventory-views-1"},
	}
	commonConfig := orgreports.InventoryViewConfig{
		InventoryMappingRule:                   &orgreports.InventoryMappingRule{Type: "inventory-per-wallet"},
		ImpairmentMethodology:                  "org-default",
		EngineVersionOverride:                  2.9,
		CostBasisCarryForwardAcquiredSide:      false,
		ProcessAcquisitionsBeforeDisposals:     true,
		UseOriginalAcquisitionDateForTransfers: true,
	}
	gaapConfig := commonConfig
	gaapConfig.CapitalizeTradingFees = true
	gaapConfig.DefaultValuationStrategy = "gaap-fair-value"
	taxConfig := commonConfig
	taxConfig.CapitalizeTradingFees = true
	return []inventoryProfile{
		{
			ID: usGAAPProfile, Jurisdiction: "US", Purpose: "books", Name: "US GAAP - Fair Value",
			Summary: "Starting profile for U.S. GAAP financial statements: FIFO lot tracking per wallet, fair-value remeasurement for in-scope crypto assets, capitalized trading fees, and the organization's pricing methodology.",
			Request: orgreports.InventoryViewCreateRequest{Name: "US GAAP - Fair Value", Config: gaapConfig, Strategy: orgreports.InventoryViewStrategy{TaxStrategy: "FIFO"}, Impair: true, IgnoreNFTs: true, IgnoreOrgWrappingTreatments: false},
			Confirmations: []string{
				"Confirm the entity actually reports under U.S. GAAP; a U.S. location alone does not establish the accounting framework.",
				"Confirm which holdings meet every ASU 2023-08 scope criterion; NFTs, issued/related-party tokens, and assets with enforceable underlying rights require separate analysis.",
				"Confirm fee treatment with the accountant; this Bitwave profile capitalizes trading fees even though some accounting guidance or entity policies may require expense treatment.",
				"The valuation pricing methodology explicitly inherits the organization's configured default; verify that organization policy before running the view.",
				"FIFO is an operational lot-selection default in this view, not a FASB-mandated election.",
			},
			Limitations: []string{"Does not determine federal, state, or local tax treatment.", "Does not cover SEC reporting, broker/dealer, investment-company, derivatives, staking, DeFi, wrapping, or transfer-control conclusions."},
			Sources:     sources, LastReviewed: "2026-08-13", AdvisoryOnly: true,
		},
		{
			ID: usFederalTaxFIFOProfile, Jurisdiction: "US", Purpose: "tax", Name: "US Federal Tax - FIFO by Wallet",
			Summary: "Starting profile for U.S. federal tax cost basis: FIFO with separate inventory per wallet/account and trading transaction costs reflected in basis/proceeds.",
			Request: orgreports.InventoryViewCreateRequest{Name: "US Federal Tax - FIFO by Wallet", Config: taxConfig, Strategy: orgreports.InventoryViewStrategy{TaxStrategy: "FIFO"}, Impair: false, IgnoreNFTs: false, IgnoreOrgWrappingTreatments: false},
			Confirmations: []string{
				"Confirm taxpayer and entity classification, tax year, asset character, and every federal, state, and local filing jurisdiction.",
				"For dispositions on or after January 1, 2025, maintain basis by wallet/account; do not use a universal multi-wallet pool.",
				"Specific identification may be appropriate only when timely identification and adequate substantiating records exist; otherwise surface FIFO as the federal default.",
				"Confirm transaction-cost allocation: acquisition, disposition, exchange, and wallet-transfer costs do not all receive identical treatment.",
			},
			Limitations: []string{"Federal starting point only; state/local conformity and entity-specific rules are not modeled.", "Does not decide capital versus ordinary character, dealer/trader status, section 1256 treatment, or reporting-form obligations."},
			Sources:     sources, LastReviewed: "2026-08-13", AdvisoryOnly: true,
		},
	}
}
