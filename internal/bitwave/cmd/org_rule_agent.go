package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
	"github.com/bitwave-io/bitwave-cli/internal/rulerecipes"
)

type agentRuleSpec struct {
	Preset                 string `json:"preset"`
	ID                     string `json:"id,omitempty"`
	Name                   string `json:"name,omitempty"`
	Priority               int    `json:"priority,omitempty"`
	AccountingConnection   string `json:"accountingConnection,omitempty"`
	AccountingConnectionID string `json:"accountingConnectionId,omitempty"`
	Category               string `json:"category,omitempty"`
	CategoryID             string `json:"categoryId,omitempty"`
	Contact                string `json:"contact,omitempty"`
	ContactID              string `json:"contactId,omitempty"`
	FeeCategory            string `json:"feeCategory,omitempty"`
	FeeCategoryID          string `json:"feeCategoryId,omitempty"`
	FeeContact             string `json:"feeContact,omitempty"`
	FeeContactID           string `json:"feeContactId,omitempty"`
	Asset                  string `json:"asset,omitempty"`
	Direction              string `json:"direction,omitempty"`
	Wallet                 string `json:"wallet,omitempty"`
	WalletID               string `json:"walletId,omitempty"`
	FromAddress            string `json:"fromAddress,omitempty"`
	ToAddress              string `json:"toAddress,omitempty"`
	FromDate               string `json:"fromDate,omitempty"`
	ToDate                 string `json:"toDate,omitempty"`
	Enabled                bool   `json:"enabled,omitempty"`
	MultiToken             *bool  `json:"multiToken,omitempty"`
	AutoCategorizeFee      *bool  `json:"autoCategorizeFee,omitempty"`
	AllowMismatch          *bool  `json:"allowMismatch,omitempty"`
	IgnoreFailPricing      bool   `json:"ignoreFailPricing,omitempty"`
}

type ruleAgentFlags struct {
	transactionMutationFlags
	spec                agentRuleSpec
	input               string
	query               string
	limit               int
	sampleLimit         int
	multiToken          bool
	singleToken         bool
	autoCategorizeFee   bool
	noAutoCategorizeFee bool
	allowMismatch       bool
	strictMatch         bool
}

type ruleResources struct {
	Org         *orgreports.OrgDetails
	Wallets     []orgreports.Wallet
	Categories  []orgreports.Category
	Contacts    []orgreports.Contact
	Connections []orgreports.AccountingConnection
}

type resolvedRulePlan struct {
	Spec       agentRuleSpec      `json:"spec"`
	Recipe     rulerecipes.Recipe `json:"recipe"`
	Payload    json.RawMessage    `json:"payload"`
	Resolution map[string]any     `json:"resolution"`
	Samples    any                `json:"samples,omitempty"`
	NextToken  string             `json:"nextToken,omitempty"`
}

func newRuleRecipesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recipes [PRESET]",
		Short: "Return compact, versioned Bitwave rule knowledge for an LLM",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var recipes any = rulerecipes.List()
			if len(args) == 1 {
				recipe, ok := rulerecipes.Find(args[0])
				if !ok {
					return fmt.Errorf("unknown rule preset %q", args[0])
				}
				recipes = recipe
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": rulerecipes.SchemaVersion, "source": rulerecipes.SourceURL,
				"lastVerified": rulerecipes.LastVerified, "recipes": recipes,
				"agentWorkflow": []string{"context", "plan", "apply"},
			})
		},
	}
}

func newRuleContextCmd() *cobra.Command {
	var f ruleAgentFlags
	f.limit = 10
	f.sampleLimit = 5
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Discover only the organization context relevant to a proposed rule",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, ok := rulerecipes.Find(f.spec.Preset); !ok {
				return fmt.Errorf("--preset must be one of: %s", strings.Join(recipeNames(), ", "))
			}
			if f.limit < 1 || f.limit > 100 || f.sampleLimit < 0 || f.sampleLimit > 25 {
				return errors.New("--limit must be 1-100 and --sample-limit must be 0-25")
			}
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			resources, err := loadRuleResources(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			connectionFilter := f.spec.AccountingConnectionID
			if f.spec.AccountingConnection != "" {
				connection, resolveErr := resolveAccountingConnection(f.spec.AccountingConnection, resources.Connections)
				if resolveErr != nil {
					return resolveErr
				}
				connectionFilter = connection.ID
			}
			categories := filterCategories(resources.Categories, f.query, connectionFilter, false)
			contacts := filterContacts(resources.Contacts, f.query, connectionFilter, false)
			if len(categories) > f.limit {
				categories = categories[:f.limit]
			}
			if len(contacts) > f.limit {
				contacts = contacts[:f.limit]
			}
			samples, next, sampleErr := ruleSamples(cmd.Context(), client, orgID, resources, f.spec, f.sampleLimit)
			warnings := []string{}
			if sampleErr != nil {
				warnings = append(warnings, "Transaction samples unavailable: "+sampleErr.Error())
			}
			recipe, _ := rulerecipes.Find(f.spec.Preset)
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": "1", "organization": orgID, "recipe": recipe,
				"accountingConnections": resources.Connections, "wallets": matchingWallets(resources.Wallets, f.spec.Wallet, f.limit),
				"categories": categories, "contacts": contacts, "samples": samples, "nextToken": next,
				"filters": f.spec, "warnings": warnings,
			})
		},
	}
	addRuleAgentFlags(cmd, &f, false)
	cmd.Flags().StringVar(&f.query, "query", "", "Return category/contact names containing this text")
	cmd.Flags().IntVar(&f.limit, "limit", f.limit, "Maximum choices of each type to return")
	cmd.Flags().IntVar(&f.sampleLimit, "sample-limit", f.sampleLimit, "Maximum representative transactions to return (0-25)")
	return cmd
}

func newRulePlanCmd() *cobra.Command {
	var f ruleAgentFlags
	f.sampleLimit = 5
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Resolve labels and produce the exact rule payload without changing Bitwave",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plans, orgID, err := prepareAgentRulePlans(cmd, &f)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": "1", "status": "preview", "operation": "plan-rules",
				"organization": orgID, "count": len(plans), "plans": plans,
			})
		},
	}
	addRuleAgentFlags(cmd, &f, true)
	cmd.Flags().IntVar(&f.sampleLimit, "sample-limit", f.sampleLimit, "Maximum matching transactions included per plan (0-25)")
	return cmd
}

func newRuleApplyCmd() *cobra.Command {
	var f ruleAgentFlags
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Resolve and create one or more recipe-backed rules in one CLI process",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plans, orgID, err := prepareAgentRulePlans(cmd, &f)
			if err != nil {
				return mutationError(cmd, "apply-rules", f.jsonOutput, err)
			}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"schemaVersion": "1", "status": "preview", "operation": "apply-rules",
					"organization": orgID, "count": len(plans), "plans": plans,
				})
			}
			if !f.yes {
				return mutationError(cmd, "apply-rules", f.jsonOutput, errors.New("refusing to create organization rules without --yes (use --dry-run to preview)"))
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
			results := make([]map[string]any, 0, len(plans))
			for index, plan := range plans {
				result, createErr := client.CreateRule(cmd.Context(), orgID, plan.Payload)
				item := map[string]any{"index": index, "name": plan.Spec.Name, "preset": plan.Spec.Preset, "success": createErr == nil}
				if createErr != nil {
					item["error"] = createErr.Error()
					results = append(results, item)
					_ = writeJSON(cmd.OutOrStdout(), map[string]any{
						"schemaVersion": "1", "status": "partial", "operation": "apply-rules",
						"organization": orgID, "created": index, "results": results,
					})
					return fmt.Errorf("apply rule %d (%s): %w", index+1, plan.Spec.Name, createErr)
				}
				item["result"] = result
				results = append(results, item)
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": "1", "status": "success", "operation": "apply-rules",
				"organization": orgID, "created": len(results), "results": results,
				"warning": "The current createRule response does not include the new rule ID; no full-list lookup was performed.",
			})
		},
	}
	addRuleAgentFlags(cmd, &f, true)
	return cmd
}

func addRuleAgentFlags(cmd *cobra.Command, f *ruleAgentFlags, includeMutation bool) {
	cmd.Flags().StringVar(&f.orgID, "org", "", "Organization ID override")
	cmd.Flags().StringVar(&f.spec.Preset, "preset", "", "Rule recipe name")
	cmd.Flags().StringVar(&f.spec.ID, "id", "", "Existing rule ID for an upsert-style save")
	cmd.Flags().StringVar(&f.spec.Name, "name", "", "Rule name (generated when omitted)")
	cmd.Flags().IntVar(&f.spec.Priority, "priority", 1, "Rule priority (1-10)")
	cmd.Flags().StringVar(&f.spec.AccountingConnection, "accounting-connection", "", "Accounting connection ID or exact name")
	cmd.Flags().StringVar(&f.spec.AccountingConnectionID, "accounting-connection-id", "", "Stable accounting connection ID (skips connection discovery)")
	cmd.Flags().StringVar(&f.spec.Category, "category", "", "Category ID or exact name")
	cmd.Flags().StringVar(&f.spec.CategoryID, "category-id", "", "Stable category ID (skips category discovery)")
	cmd.Flags().StringVar(&f.spec.Contact, "contact", "", "Contact ID or exact name")
	cmd.Flags().StringVar(&f.spec.ContactID, "contact-id", "", "Stable contact ID (skips contact discovery)")
	cmd.Flags().StringVar(&f.spec.FeeCategory, "fee-category", "", "Fee category ID or exact name")
	cmd.Flags().StringVar(&f.spec.FeeCategoryID, "fee-category-id", "", "Stable fee category ID (skips category discovery)")
	cmd.Flags().StringVar(&f.spec.FeeContact, "fee-contact", "", "Fee contact ID or exact name")
	cmd.Flags().StringVar(&f.spec.FeeContactID, "fee-contact-id", "", "Stable fee contact ID (skips contact discovery)")
	cmd.Flags().StringVar(&f.spec.Asset, "asset", "", "Rule coin/ticker")
	cmd.Flags().StringVar(&f.spec.Direction, "direction", "", "Inbound, Outbound, All, Empty, or NA")
	cmd.Flags().StringVar(&f.spec.Wallet, "wallet", "", "Wallet ID or exact name")
	cmd.Flags().StringVar(&f.spec.WalletID, "wallet-id", "", "Stable wallet ID (skips wallet discovery)")
	cmd.Flags().StringVar(&f.spec.FromAddress, "from-address", "", "Exact primary sender address")
	cmd.Flags().StringVar(&f.spec.ToAddress, "to-address", "", "Exact primary recipient address")
	cmd.Flags().StringVar(&f.spec.FromDate, "from-date", "", "Inclusive rule start date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&f.spec.ToDate, "to-date", "", "Inclusive rule end date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&f.spec.Enabled, "enabled", false, "Create the rule enabled")
	cmd.Flags().BoolVar(&f.multiToken, "multi-token", false, "Override the recipe to handle multi-token transactions")
	cmd.Flags().BoolVar(&f.singleToken, "single-token", false, "Override the recipe to require a single-token transaction")
	cmd.Flags().BoolVar(&f.autoCategorizeFee, "auto-categorize-fee", false, "Override the recipe to categorize fees")
	cmd.Flags().BoolVar(&f.noAutoCategorizeFee, "no-auto-categorize-fee", false, "Override the recipe not to categorize fees")
	cmd.Flags().BoolVar(&f.allowMismatch, "allow-mismatch", false, "Allow action/value mismatches")
	cmd.Flags().BoolVar(&f.strictMatch, "strict-match", false, "Reject action/value mismatches")
	cmd.Flags().BoolVar(&f.spec.IgnoreFailPricing, "ignore-fail-pricing", false, "Allow categorization when pricing fails")
	if includeMutation {
		cmd.Flags().StringVarP(&f.input, "input", "i", "", "One rule spec or an array of specs in JSON; flags are used when omitted")
		cmd.Flags().BoolVar(&f.yes, "yes", false, "Confirm creation of all planned rules")
		cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "Resolve and print exact payloads without creating rules")
		cmd.Flags().BoolVar(&f.jsonOutput, "json", true, "Emit machine-readable JSON")
	}
}

func prepareAgentRulePlans(cmd *cobra.Command, f *ruleAgentFlags) ([]resolvedRulePlan, string, error) {
	if f.multiToken && f.singleToken {
		return nil, "", errors.New("--multi-token and --single-token are mutually exclusive")
	}
	if f.autoCategorizeFee && f.noAutoCategorizeFee {
		return nil, "", errors.New("--auto-categorize-fee and --no-auto-categorize-fee are mutually exclusive")
	}
	if f.allowMismatch && f.strictMatch {
		return nil, "", errors.New("--allow-mismatch and --strict-match are mutually exclusive")
	}
	specs, err := readAgentRuleSpecs(f.input, f.spec, cmd.InOrStdin())
	if err != nil {
		return nil, "", err
	}
	if len(specs) == 0 || len(specs) > 100 {
		return nil, "", errors.New("rule plan must contain between 1 and 100 specs")
	}
	if f.input == "" {
		applyBooleanOverrides(&specs[0], f)
	}
	orgID, err := resolveReportOrg(f.orgID)
	if err != nil {
		return nil, "", err
	}
	client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(orgID))
	resources, err := loadRuleResourcesForSpecs(cmd.Context(), client, orgID, specs, f.sampleLimit)
	if err != nil {
		return nil, "", err
	}
	plans := make([]resolvedRulePlan, 0, len(specs))
	for index, spec := range specs {
		if spec.Name == "" {
			spec.Name = generatedRuleName(spec, index)
		}
		plan, err := resolveAgentRulePlan(cmd.Context(), client, orgID, resources, spec, f.sampleLimit)
		if err != nil {
			return nil, "", fmt.Errorf("plan %d: %w", index+1, err)
		}
		plans = append(plans, plan)
	}
	return plans, orgID, nil
}

func applyBooleanOverrides(spec *agentRuleSpec, f *ruleAgentFlags) {
	if f.multiToken {
		value := true
		spec.MultiToken = &value
	}
	if f.singleToken {
		value := false
		spec.MultiToken = &value
	}
	if f.autoCategorizeFee {
		value := true
		spec.AutoCategorizeFee = &value
	}
	if f.noAutoCategorizeFee {
		value := false
		spec.AutoCategorizeFee = &value
	}
	if f.allowMismatch {
		value := true
		spec.AllowMismatch = &value
	}
	if f.strictMatch {
		value := false
		spec.AllowMismatch = &value
	}
}

func readAgentRuleSpecs(path string, flags agentRuleSpec, stdin io.Reader) ([]agentRuleSpec, error) {
	if path == "" {
		if flags.Preset == "" {
			return nil, errors.New("--preset is required when --input is omitted")
		}
		return []agentRuleSpec{flags}, nil
	}
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(io.LimitReader(stdin, 8<<20))
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read rule plan: %w", err)
	}
	var specs []agentRuleSpec
	if err := json.Unmarshal(data, &specs); err == nil {
		return specs, nil
	}
	var spec agentRuleSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("rule plan input must be one JSON object or an array: %w", err)
	}
	return []agentRuleSpec{spec}, nil
}

func generatedRuleName(spec agentRuleSpec, index int) string {
	parts := []string{"CLI", spec.Preset}
	if spec.Asset != "" {
		parts = append(parts, strings.ToUpper(spec.Asset))
	}
	if spec.Wallet != "" {
		parts = append(parts, spec.Wallet)
	}
	parts = append(parts, time.Now().UTC().Format("20060102-150405"))
	if index > 0 {
		parts = append(parts, fmt.Sprintf("%02d", index+1))
	}
	return strings.Join(parts, " - ")
}

func loadRuleResources(ctx context.Context, client *orgreports.Client, orgID string) (*ruleResources, error) {
	return loadSelectedRuleResources(ctx, client, orgID, true, true, true, true, true)
}

func loadRuleResourcesForSpecs(ctx context.Context, client *orgreports.Client, orgID string, specs []agentRuleSpec, sampleLimit int) (*ruleResources, error) {
	needOrg := sampleLimit > 0
	needWallets := false
	needCategories := false
	needContacts := false
	needConnections := false
	for _, spec := range specs {
		needOrg = needOrg || spec.FromDate != "" || spec.ToDate != ""
		needWallets = needWallets || spec.Wallet != ""
		needCategories = needCategories || spec.Category != "" || spec.FeeCategory != ""
		needContacts = needContacts || spec.Contact != "" || spec.FeeContact != ""
		hasAccountingItemID := spec.CategoryID != "" || spec.ContactID != "" || spec.FeeCategoryID != "" || spec.FeeContactID != ""
		needConnections = needConnections || spec.AccountingConnection != "" || (spec.AccountingConnectionID == "" && !hasAccountingItemID)
	}
	return loadSelectedRuleResources(ctx, client, orgID, needOrg, needWallets, needCategories, needContacts, needConnections)
}

func loadSelectedRuleResources(ctx context.Context, client *orgreports.Client, orgID string, needOrg, needWallets, needCategories, needContacts, needConnections bool) (*ruleResources, error) {
	resources := &ruleResources{}
	type loadResult struct {
		kind  string
		value any
		err   error
	}
	count := 0
	for _, needed := range []bool{needOrg, needWallets, needCategories, needContacts, needConnections} {
		if needed {
			count++
		}
	}
	results := make(chan loadResult, count)
	var wg sync.WaitGroup
	load := func(kind string, fn func() (any, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := fn()
			results <- loadResult{kind: kind, value: value, err: err}
		}()
	}
	if needOrg {
		load("organization", func() (any, error) { return client.Org(ctx, orgID) })
	}
	if needWallets {
		load("wallets", func() (any, error) { return client.Wallets(ctx, orgID) })
	}
	if needCategories {
		load("categories", func() (any, error) { return client.Categories(ctx, orgID) })
	}
	if needContacts {
		load("contacts", func() (any, error) { return client.Contacts(ctx, orgID) })
	}
	if needConnections {
		load("connections", func() (any, error) { return client.AccountingConnections(ctx, orgID) })
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	for result := range results {
		if result.err != nil {
			return nil, fmt.Errorf("load rule %s: %w", result.kind, result.err)
		}
		switch result.kind {
		case "organization":
			resources.Org = result.value.(*orgreports.OrgDetails)
		case "wallets":
			resources.Wallets = result.value.([]orgreports.Wallet)
		case "categories":
			resources.Categories = result.value.([]orgreports.Category)
		case "contacts":
			resources.Contacts = result.value.([]orgreports.Contact)
		case "connections":
			resources.Connections = result.value.([]orgreports.AccountingConnection)
		}
	}
	return resources, nil
}

func resolveAgentRulePlan(ctx context.Context, client *orgreports.Client, orgID string, resources *ruleResources, spec agentRuleSpec, sampleLimit int) (resolvedRulePlan, error) {
	recipe, ok := rulerecipes.Find(spec.Preset)
	if !ok {
		return resolvedRulePlan{}, fmt.Errorf("unknown preset %q; choose one of: %s", spec.Preset, strings.Join(recipeNames(), ", "))
	}
	if !recipe.ApplySupported {
		return resolvedRulePlan{}, fmt.Errorf("preset %q is guidance-only; inspect `bitwave rule recipes %s`", recipe.Name, recipe.Name)
	}
	if (spec.AccountingConnection != "" && spec.AccountingConnectionID != "") || (spec.Category != "" && spec.CategoryID != "") || (spec.Contact != "" && spec.ContactID != "") || (spec.FeeCategory != "" && spec.FeeCategoryID != "") || (spec.FeeContact != "" && spec.FeeContactID != "") || (spec.Wallet != "" && spec.WalletID != "") {
		return resolvedRulePlan{}, errors.New("use either a human-readable reference or its matching --*-id flag, not both")
	}
	connection, err := resolveAccountingConnection(spec.AccountingConnection, resources.Connections)
	if err != nil && spec.AccountingConnection != "" {
		return resolvedRulePlan{}, err
	}
	connectionID := spec.AccountingConnectionID
	if connection != nil {
		connectionID = connection.ID
	}
	category, err := resolveCategory(spec.Category, connectionID, resources.Categories)
	if err != nil {
		return resolvedRulePlan{}, err
	}
	contact, err := resolveContact(spec.Contact, connectionID, resources.Contacts)
	if err != nil {
		return resolvedRulePlan{}, err
	}
	feeCategory, err := resolveCategory(spec.FeeCategory, connectionID, resources.Categories)
	if err != nil {
		return resolvedRulePlan{}, fmt.Errorf("fee %w", err)
	}
	feeContact, err := resolveContact(spec.FeeContact, connectionID, resources.Contacts)
	if err != nil {
		return resolvedRulePlan{}, fmt.Errorf("fee %w", err)
	}
	// Simple recipes use the primary category/contact for fees unless the
	// caller deliberately selects separate fee accounting.
	categoryID, contactID := spec.CategoryID, spec.ContactID
	feeCategoryID, feeContactID := spec.FeeCategoryID, spec.FeeContactID
	if category != nil {
		categoryID = category.ID
	}
	if contact != nil {
		contactID = contact.ID
	}
	if feeCategory != nil {
		feeCategoryID = feeCategory.ID
	}
	if feeContact != nil {
		feeContactID = feeContact.ID
	}
	if recipe.ActionType == "SimpleCategorization" {
		if feeCategoryID == "" {
			feeCategoryID = categoryID
		}
		if feeContactID == "" {
			feeContactID = contactID
		}
	}
	if connectionID == "" {
		derived := map[string]bool{}
		for _, id := range []string{categoryID, contactID, feeCategoryID, feeContactID} {
			if prefix := connectionFromItemID(id); prefix != "" {
				derived[prefix] = true
			}
		}
		if len(derived) == 1 {
			for id := range derived {
				connectionID = id
			}
		} else if len(derived) > 1 {
			return resolvedRulePlan{}, errors.New("selected category/contact IDs belong to different accounting connections")
		}
	}
	connectionID, err = inferConnectionID(connectionID, category, contact, feeCategory, feeContact, resources.Connections)
	if err != nil {
		return resolvedRulePlan{}, err
	}
	walletID := spec.WalletID
	if spec.Wallet != "" {
		ids, err := resolveWalletRefs([]string{spec.Wallet}, resources.Wallets)
		if err != nil {
			return resolvedRulePlan{}, err
		}
		walletID = ids[0]
	}
	timezone := "UTC"
	if resources.Org != nil && resources.Org.Timezone != "" {
		timezone = resources.Org.Timezone
	}
	after, before, err := resolveRuleDates(spec.FromDate, spec.ToDate, timezone)
	if err != nil {
		return resolvedRulePlan{}, err
	}
	plan := rulerecipes.Plan{
		Preset: spec.Preset, ID: spec.ID, Name: spec.Name, Priority: spec.Priority,
		AccountingConnectionID: connectionID, Asset: strings.ToUpper(strings.TrimSpace(spec.Asset)), Direction: canonicalRuleDirection(spec.Direction),
		WalletID: walletID, FromAddress: spec.FromAddress, ToAddress: spec.ToAddress,
		AfterDateSEC: after, BeforeDateSEC: before, Enabled: spec.Enabled,
		MultiToken: spec.MultiToken, AutoCategorizeFee: spec.AutoCategorizeFee, AllowMismatch: spec.AllowMismatch,
		IgnoreFailPricing: spec.IgnoreFailPricing,
	}
	plan.CategoryID = categoryID
	plan.ContactID = contactID
	plan.FeeCategoryID = feeCategoryID
	plan.FeeContactID = feeContactID
	payload, err := rulerecipes.Build(plan)
	if err != nil {
		return resolvedRulePlan{}, err
	}
	samples, nextToken, sampleErr := ruleSamples(ctx, client, orgID, resources, spec, sampleLimit)
	if sampleErr != nil {
		return resolvedRulePlan{}, fmt.Errorf("sample matching transactions: %w", sampleErr)
	}
	resolution := map[string]any{"accountingConnectionId": connectionID, "walletId": walletID, "categoryId": categoryID, "contactId": contactID, "feeCategoryId": feeCategoryID, "feeContactId": feeContactID}
	if category != nil {
		resolution["category"] = category
	}
	if contact != nil {
		resolution["contact"] = contact
	}
	if feeCategory != nil {
		resolution["feeCategory"] = feeCategory
	}
	if feeContact != nil {
		resolution["feeContact"] = feeContact
	}
	return resolvedRulePlan{Spec: spec, Recipe: recipe, Payload: payload, Resolution: resolution, Samples: samples, NextToken: nextToken}, nil
}

func resolveRuleDates(from, to, timezone string) (int64, int64, error) {
	if (from == "") != (to == "") {
		return 0, 0, errors.New("--from-date and --to-date must be supplied together")
	}
	if from == "" {
		return 0, 0, nil
	}
	if err := validateExportDateRange(from, to, false); err != nil {
		return 0, 0, err
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return 0, 0, fmt.Errorf("load organization timezone %q: %w", timezone, err)
	}
	start, err := time.ParseInLocation("2006-01-02", from, location)
	if err != nil {
		return 0, 0, err
	}
	end, err := time.ParseInLocation("2006-01-02", to, location)
	if err != nil {
		return 0, 0, err
	}
	return start.Unix(), end.Add(24*time.Hour - time.Second).Unix(), nil
}

func canonicalRuleDirection(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "inbound", "inflow", "incoming", "receive":
		return "Inbound"
	case "outbound", "outflow", "outgoing", "send":
		return "Outbound"
	case "all":
		return "All"
	case "empty", "blank":
		return "Empty"
	case "na", "n/a":
		return "NA"
	default:
		return value
	}
}

func resolveAccountingConnection(ref string, items []orgreports.AccountingConnection) (*orgreports.AccountingConnection, error) {
	if ref == "" {
		return nil, nil
	}
	ref = strings.TrimSpace(ref)
	matches := make([]orgreports.AccountingConnection, 0)
	for _, item := range items {
		if item.Disabled {
			continue
		}
		if item.ID == ref || strings.EqualFold(item.Name, ref) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("accounting connection %q was not found", ref)
	}
	return nil, fmt.Errorf("accounting connection %q is ambiguous; pass its ID", ref)
}

func resolveCategory(ref, connectionID string, items []orgreports.Category) (*orgreports.Category, error) {
	if ref == "" {
		return nil, nil
	}
	ref = strings.TrimSpace(ref)
	matches := make([]orgreports.Category, 0)
	for _, item := range items {
		if !item.Enabled || (connectionID != "" && item.AccountingConnectionID != connectionID) {
			continue
		}
		if item.ID == ref || strings.EqualFold(item.Name, ref) || (item.Code != "" && strings.EqualFold(item.Code, ref)) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("category %q was not found", ref)
	}
	return nil, fmt.Errorf("category %q is ambiguous; pass --accounting-connection or the category ID", ref)
}

func resolveContact(ref, connectionID string, items []orgreports.Contact) (*orgreports.Contact, error) {
	if ref == "" {
		return nil, nil
	}
	ref = strings.TrimSpace(ref)
	matches := make([]orgreports.Contact, 0)
	for _, item := range items {
		if !item.Enabled || (connectionID != "" && item.AccountingConnectionID != connectionID) {
			continue
		}
		if item.ID == ref || strings.EqualFold(item.Name, ref) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("contact %q was not found", ref)
	}
	return nil, fmt.Errorf("contact %q is ambiguous; pass --accounting-connection or the contact ID", ref)
}

func inferConnectionID(explicit string, category *orgreports.Category, contact *orgreports.Contact, feeCategory *orgreports.Category, feeContact *orgreports.Contact, connections []orgreports.AccountingConnection) (string, error) {
	ids := map[string]bool{}
	if explicit != "" {
		ids[explicit] = true
	}
	for _, id := range []string{categoryConnection(category), contactConnection(contact), categoryConnection(feeCategory), contactConnection(feeContact)} {
		if id != "" {
			ids[id] = true
		}
	}
	if len(ids) > 1 {
		return "", errors.New("selected category/contact values belong to different accounting connections")
	}
	for id := range ids {
		return id, nil
	}
	enabled := make([]orgreports.AccountingConnection, 0)
	for _, item := range connections {
		if !item.Disabled {
			enabled = append(enabled, item)
		}
	}
	if len(enabled) == 1 {
		return enabled[0].ID, nil
	}
	return "", errors.New("accounting connection could not be inferred; pass --accounting-connection")
}

func categoryConnection(item *orgreports.Category) string {
	if item == nil {
		return ""
	}
	return item.AccountingConnectionID
}

func contactConnection(item *orgreports.Contact) string {
	if item == nil {
		return ""
	}
	return item.AccountingConnectionID
}

func connectionFromItemID(id string) string {
	if at := strings.LastIndex(strings.TrimSpace(id), "."); at > 0 {
		return id[:at]
	}
	return ""
}

func ruleSamples(ctx context.Context, client *orgreports.Client, orgID string, resources *ruleResources, spec agentRuleSpec, limit int) (any, string, error) {
	if limit == 0 {
		return nil, "", nil
	}
	if (spec.FromDate == "") != (spec.ToDate == "") {
		return nil, "", errors.New("fromDate and toDate must be supplied together")
	}
	if spec.FromDate != "" {
		if err := validateExportDateRange(spec.FromDate, spec.ToDate, false); err != nil {
			return nil, "", err
		}
	}
	walletIDs := []string{}
	if spec.WalletID != "" {
		walletIDs = []string{spec.WalletID}
	} else if spec.Wallet != "" {
		resolved, err := resolveWalletRefs([]string{spec.Wallet}, resources.Wallets)
		if err != nil {
			return nil, "", err
		}
		walletIDs = resolved
	}
	filters := orgreports.TransactionExportFilters{
		WalletIDs: walletIDs, FromAddresses: uniqueNonEmpty([]string{spec.FromAddress}),
		ToAddresses: uniqueNonEmpty([]string{spec.ToAddress}), DateRange: optionalDateRange(spec.FromDate, spec.ToDate),
	}
	if spec.Asset != "" {
		// Rule assets are coin/ticker values while search asset filters are
		// internal asset IDs. Free-text search safely finds ticker-bearing rows.
		filters.SearchTokens = []string{spec.Asset}
	}
	direction := canonicalRuleDirection(spec.Direction)
	if direction == "" {
		if recipe, ok := rulerecipes.Find(spec.Preset); ok {
			direction = recipe.DefaultDirection
		}
	}
	switch direction {
	case "Inbound":
		filters.TransactionTypes = []string{"receive"}
	case "Outbound":
		filters.TransactionTypes = []string{"send"}
	case "Empty":
		filters.TransactionTypes = []string{"contract-execution"}
	}
	response, err := client.SearchTransactions(ctx, orgID, orgreports.TransactionSearchRequest{
		Timezone: resources.Org.Timezone, Limit: limit, SortBy: "timestamp", SortDirection: "desc", Filters: filters,
	})
	if err != nil {
		return nil, "", err
	}
	return compactTransactions(response.Transactions), response.NextToken, nil
}

func matchingWallets(items []orgreports.Wallet, query string, limit int) []orgreports.Wallet {
	query = strings.ToLower(strings.TrimSpace(query))
	result := make([]orgreports.Wallet, 0)
	for _, item := range items {
		if query != "" && !strings.EqualFold(item.ID, query) && !strings.Contains(strings.ToLower(item.Name), query) {
			continue
		}
		result = append(result, item)
		if len(result) == limit {
			break
		}
	}
	return result
}

func recipeNames() []string {
	items := rulerecipes.List()
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	sort.Strings(names)
	return names
}
