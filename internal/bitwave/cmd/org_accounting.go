package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/apierr"
	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

var chartAccountTypes = []string{"asset", "bank", "equity", "expense", "liability", "other", "revenue"}

type accountingReadiness struct {
	AccountingConnectionReady bool                              `json:"accountingConnectionReady"`
	BuiltInDigitalAssets      bool                              `json:"builtInDigitalAssets"`
	ReadyForRules             bool                              `json:"readyForRules"`
	Decision                  string                            `json:"decision"`
	InteractionRequired       bool                              `json:"interactionRequired"`
	Connections               []orgreports.AccountingConnection `json:"connections"`
	ConnectionCount           int                               `json:"connectionCount"`
	AdditionalCategoryCount   int                               `json:"additionalCategoryCount"`
	ContactCount              int                               `json:"contactCount"`
	Starter                   accountingStarterPolicy           `json:"starter"`
	Prompt                    map[string]any                    `json:"prompt,omitempty"`
	NextCommands              []string                          `json:"nextCommands"`
}

type chartAccountInput struct {
	ConnectionID string `json:"connectionId"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Code         string `json:"code"`
	Description  string `json:"description"`
}

type accountingStarterPolicy struct {
	AutomaticAccounts []string                        `json:"automaticAccounts"`
	Categories        []chartAccountInput             `json:"categories"`
	Contacts          []orgreports.CreateContactInput `json:"contacts"`
	Guardrails        []string                        `json:"guardrails"`
}

func newOrgAccountingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accounting",
		Short: "Prepare an accounting connection and client-specific accounts before categorization",
		Long: `Inspect accounting readiness before creating categorization rules.

If no connection exists, an LLM should ask one concise question: connect the
organization's external accounting system in Bitwave, or create a manual
Bitwave accounting connection. Bitwave supplies the Digital Assets account
automatically. External provider authorization remains in the Bitwave web app;
manual setup and imports of additional client-specific accounts are available
here.`,
	}
	cmd.AddCommand(newOrgAccountingStatusCmd(), newOrgAccountingConnectionsCmd(), newOrgAccountingManualCmd(), newOrgAccountingAccountsCmd(), newOrgAccountingStarterCmd())
	return cmd
}

func starterPolicy(connectionID string) accountingStarterPolicy {
	return accountingStarterPolicy{
		AutomaticAccounts: []string{"Digital Assets"},
		Categories: []chartAccountInput{
			{ConnectionID: connectionID, ID: "bitwave-starter-general-revenue", Code: "BW-4000", Name: "General Revenue", Type: "revenue", Description: "Starter fallback; replace with the client's revenue accounts when supplied"},
			{ConnectionID: connectionID, ID: "bitwave-starter-general-expense", Code: "BW-6000", Name: "General Expense", Type: "expense", Description: "Starter fallback; replace with the client's expense accounts when supplied"},
			{ConnectionID: connectionID, ID: "bitwave-starter-gas-fees", Code: "BW-6100", Name: "Gas Fees", Type: "expense", Description: "Standalone network and contract-execution gas fees"},
		},
		Contacts: []orgreports.CreateContactInput{
			{ConnectionID: connectionID, RemoteID: "bitwave-starter-general-customer", Name: "General Customer", Type: "Customer"},
			{ConnectionID: connectionID, RemoteID: "bitwave-starter-general-vendor", Name: "General Vendor", Type: "Vendor"},
			{ConnectionID: connectionID, RemoteID: "bitwave-starter-gas-fees", Name: "Gas Fees", Type: "Vendor"},
		},
		Guardrails: []string{
			"Never create another Digital Assets account or token/network/protocol-specific asset accounts unless the user explicitly requests them.",
			"Treat starter revenue and expense resources as fallbacks, not inferred accounting policy.",
			"Trade rules use the Gas Fees contact with no fee category and autoCategorizeFee=false.",
			"Require the user to specify or approve every additional category and contact.",
		},
	}
}

func newOrgAccountingStatusCmd() *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Return compact LLM guidance for accounting setup readiness",
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolvedOrg, client, err := accountingClient(orgID)
			if err != nil {
				return err
			}
			connections, err := client.AccountingConnections(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("list accounting connections: %w", err)
			}
			categories, err := client.Categories(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("list chart accounts: %w", err)
			}
			contacts, err := client.Contacts(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("list contacts: %w", err)
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": "1", "organization": resolvedOrg,
				"readiness": buildAccountingReadiness(connections, categories, contacts),
			})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func buildAccountingReadiness(connections []orgreports.AccountingConnection, categories []orgreports.Category, contacts []orgreports.Contact) accountingReadiness {
	active := make([]orgreports.AccountingConnection, 0, len(connections))
	activeIDs := map[string]bool{}
	for _, connection := range connections {
		if !connection.Disabled {
			active = append(active, connection)
			activeIDs[connection.ID] = true
		}
	}
	availableAccounts := 0
	for _, category := range categories {
		if category.Enabled && activeIDs[category.AccountingConnectionID] {
			availableAccounts++
		}
	}
	availableContacts := 0
	for _, contact := range contacts {
		if contact.Enabled && activeIDs[contact.AccountingConnectionID] {
			availableContacts++
		}
	}
	readiness := accountingReadiness{
		AccountingConnectionReady: len(active) > 0, BuiltInDigitalAssets: len(active) > 0,
		Connections: active, ConnectionCount: len(active), AdditionalCategoryCount: availableAccounts, ContactCount: availableContacts,
		NextCommands: []string{"bitwave org accounting status --json"},
	}
	if len(active) == 1 {
		readiness.Starter = starterPolicy(active[0].ID)
	} else {
		readiness.Starter = starterPolicy("")
	}
	switch {
	case len(active) == 0:
		readiness.Decision = "choose_accounting_setup"
		readiness.InteractionRequired = true
		readiness.Prompt = map[string]any{
			"question": "Would you like to connect your accounting system in Bitwave, or create a manual chart of accounts in Bitwave?",
			"choices": []map[string]string{
				{"id": "connect_external", "label": "Connect accounting system", "next": "Open Accounting Connections in the Bitwave web app to authorize the provider, then rerun status."},
				{"id": "manual_chart", "label": "Create chart in Bitwave", "next": "bitwave org accounting manual create --yes --json"},
			},
		}
		readiness.NextCommands = []string{"bitwave org accounting manual create --yes --json", "bitwave org accounting status --json"}
	case availableAccounts == 0 && availableContacts == 0:
		readiness.Decision = "client_categories_and_contacts_needed"
		readiness.InteractionRequired = true
		readiness.Prompt = map[string]any{
			"question": "Bitwave already provides the Digital Assets account. Which additional client-specific categorization accounts and contacts should be added?",
			"choices": []map[string]string{
				{"id": "apply_starter", "label": "Create conservative starter set", "next": "bitwave org accounting starter apply --yes --json"},
				{"id": "provide_lists", "label": "Provide accounts and contacts", "next": "Use the client's chart and counterparty list; do not invent specialized digital-asset accounts."},
				{"id": "analyze_transactions", "label": "Analyze transactions", "next": "Suggest only minimal revenue/expense categories and contacts supported by transaction evidence."},
			},
		}
		readiness.NextCommands = []string{"bitwave org accounting starter apply --dry-run --json", "bitwave org accounting starter apply --yes --json", "bitwave org accounting status --json"}
	case availableContacts == 0:
		readiness.Decision = "contacts_required"
		readiness.InteractionRequired = true
		readiness.Prompt = map[string]any{"question": "Which contacts should be created for the client's non-trade categorization and required trade fee contact?"}
		readiness.NextCommands = []string{"Create or import client contacts, including the trade fee contact.", "bitwave org accounting status --json"}
	case availableAccounts == 0:
		readiness.Decision = "client_categories_needed"
		readiness.InteractionRequired = true
		readiness.Prompt = map[string]any{"question": "Which client-specific revenue, expense, liability, or equity categories should be added? Digital Assets already exists automatically."}
		readiness.NextCommands = []string{"bitwave org accounting accounts import --input accounts.json --dry-run --json", "bitwave org accounting status --json"}
	default:
		readiness.ReadyForRules = true
		readiness.Decision = "ready_for_categorization_and_rules"
		readiness.NextCommands = []string{"bitwave rule context --preset PRESET", "bitwave transaction categorization-options --accounting-connection CONNECTION_ID --query QUERY --json"}
	}
	return readiness
}

func newOrgAccountingConnectionsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "connections", Short: "Inspect accounting connections"}
	cmd.AddCommand(newOrgAccountingConnectionsListCmd())
	return cmd
}

func newOrgAccountingConnectionsListCmd() *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use: "list", Short: "List accounting connections",
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolvedOrg, client, err := accountingClient(orgID)
			if err != nil {
				return err
			}
			connections, err := client.AccountingConnections(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("list accounting connections: %w", err)
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "connections": connections})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgAccountingManualCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "manual", Short: "Create a Bitwave-managed manual accounting connection"}
	cmd.AddCommand(newOrgAccountingManualCreateCmd())
	return cmd
}

func newOrgAccountingManualCreateCmd() *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{
		Use: "create", Short: "Create a manual connection and conservative starter categories and contacts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			operation := "create-manual-accounting-connection"
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			preview := map[string]any{"method": "POST", "path": "/orgs/" + orgID + "/connections/manual", "starter": starterPolicy("")}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: preview})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to change the organization without --yes (use --dry-run to preview)"))
			}
			_, client, err := accountingClient(orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			connections, err := client.AccountingConnections(cmd.Context(), orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("check accounting connections: %w", err))
			}
			for _, connection := range connections {
				if !connection.Disabled && (strings.Contains(strings.ToLower(connection.Type), "manual") || strings.EqualFold(connection.Name, "manual")) {
					starter, err := applyStarter(cmd, client, orgID, starterPolicy(connection.ID))
					if err != nil {
						return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("apply starter to existing manual connection: %w", err))
					}
					envelope := mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: map[string]any{"status": "skipped_existing", "connectionId": connection.ID, "starter": starter, "nextCommand": "bitwave org accounting status --json"}}
					return outputMutation(cmd, f.jsonOutput, envelope, "manual accounting connection already exists: "+connection.ID+"\n")
				}
			}
			response, err := client.CreateManualAccountingConnection(cmd.Context(), orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("create manual accounting connection: %w", err))
			}
			starter, err := applyStarter(cmd, client, orgID, starterPolicy(response.ConnectionID))
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("manual connection %s was created but starter setup failed: %w", response.ConnectionID, err))
			}
			envelope := mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: map[string]any{"connectionId": response.ConnectionID, "starter": starter, "nextCommand": "bitwave org accounting status --json"}}
			return outputMutation(cmd, f.jsonOutput, envelope, "created manual accounting connection "+response.ConnectionID+"\n")
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}

func newOrgAccountingAccountsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "accounts", Short: "List, create, or import Bitwave manual chart accounts"}
	cmd.AddCommand(newOrgAccountingAccountsListCmd(), newOrgAccountingAccountCreateCmd(), newOrgAccountingAccountsImportCmd())
	return cmd
}

func newOrgAccountingAccountsListCmd() *cobra.Command {
	var orgID, connectionID, query string
	var limit int
	cmd := &cobra.Command{
		Use: "list", Short: "List a bounded set of chart accounts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > 500 {
				return errors.New("--limit must be between 1 and 500")
			}
			resolvedOrg, client, err := accountingClient(orgID)
			if err != nil {
				return err
			}
			accounts, err := client.Categories(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("list chart accounts: %w", err)
			}
			accounts = filterCategories(accounts, query, connectionID, false)
			total := len(accounts)
			if len(accounts) > limit {
				accounts = accounts[:limit]
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "accounts": accounts, "total": total, "truncated": total > len(accounts)})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().StringVar(&connectionID, "accounting-connection", "", "Only accounts belonging to this connection ID")
	cmd.Flags().StringVar(&query, "query", "", "Case-insensitive name, code, or ID substring")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum accounts to return")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgAccountingAccountCreateCmd() *cobra.Command {
	var f transactionMutationFlags
	var input chartAccountInput
	cmd := &cobra.Command{
		Use: "create", Short: "Create one account in a manual Bitwave chart",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCreateChartAccounts(cmd, []chartAccountInput{input}, f, 1)
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringVar(&input.ConnectionID, "accounting-connection", "", "Manual accounting connection ID")
	cmd.Flags().StringVar(&input.ID, "id", "", "Stable remote/account ID")
	cmd.Flags().StringVar(&input.Name, "name", "", "Account name")
	cmd.Flags().StringVar(&input.Type, "type", "", "Account type: asset, bank, equity, expense, liability, other, or revenue")
	cmd.Flags().StringVar(&input.Code, "code", "", "Account code")
	cmd.Flags().StringVar(&input.Description, "description", "", "Account description")
	return cmd
}

func newOrgAccountingAccountsImportCmd() *cobra.Command {
	var f transactionMutationFlags
	var inputPath string
	var concurrency int
	cmd := &cobra.Command{
		Use: "import", Short: "Import a JSON array of accounts into a manual Bitwave chart",
		RunE: func(cmd *cobra.Command, _ []string) error {
			accounts, err := loadChartAccounts(inputPath, cmd.InOrStdin())
			if err != nil {
				return mutationError(cmd, "import-chart-of-accounts", f.jsonOutput, err)
			}
			return runCreateChartAccounts(cmd, accounts, f, concurrency)
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringVarP(&inputPath, "input", "i", "", "Accounts JSON file, or - for stdin (required)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 2, "Maximum concurrent account creations (1-8)")
	return cmd
}

func runCreateChartAccounts(cmd *cobra.Command, accounts []chartAccountInput, f transactionMutationFlags, concurrency int) error {
	operation := "import-chart-of-accounts"
	if len(accounts) == 0 {
		return mutationError(cmd, operation, f.jsonOutput, errors.New("at least one chart account is required"))
	}
	if concurrency < 1 || concurrency > 8 {
		return mutationError(cmd, operation, f.jsonOutput, errors.New("--concurrency must be between 1 and 8"))
	}
	for i := range accounts {
		if err := validateChartAccount(accounts[i]); err != nil {
			return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("account %d: %w", i+1, err))
		}
	}
	orgID, err := resolveReportOrg(f.orgID)
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, err)
	}
	requests := make([]orgreports.CreateChartAccountInput, 0, len(accounts))
	for _, account := range accounts {
		requests = append(requests, accountRequest(account))
	}
	preview := map[string]any{"method": "POST", "path": "/org/" + orgID + "/categories", "requests": requests}
	if f.dryRun {
		return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: preview})
	}
	if !f.yes {
		return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to change the organization without --yes (use --dry-run to preview)"))
	}
	_, client, err := accountingClient(orgID)
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, err)
	}
	connections, err := client.AccountingConnections(cmd.Context(), orgID)
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("validate accounting connection: %w", err))
	}
	connectionTypes := map[string]string{}
	for _, connection := range connections {
		if !connection.Disabled {
			connectionTypes[connection.ID] = connection.Type
		}
	}
	for _, account := range accounts {
		connectionType, ok := connectionTypes[account.ConnectionID]
		if !ok {
			return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("accounting connection %q was not found or is disabled", account.ConnectionID))
		}
		if !strings.Contains(strings.ToLower(connectionType), "manual") {
			return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("accounting connection %q is not manual; create or sync its chart in the external accounting system", account.ConnectionID))
		}
	}
	results := make([]map[string]any, len(requests))
	existingAccounts, err := client.Categories(cmd.Context(), orgID)
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("check existing chart accounts: %w", err))
	}
	existingIDs := map[string]bool{}
	for _, account := range existingAccounts {
		existingIDs[account.ID] = true
	}
	jobs := make(chan int)
	workerCount := min(concurrency, len(requests))
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				response, createErr := createChartAccountWithRetry(cmd, client, orgID, requests[i])
				if createErr != nil {
					results[i] = map[string]any{"input": accounts[i], "status": "failed", "error": createErr.Error()}
					continue
				}
				results[i] = map[string]any{"input": accounts[i], "status": "created", "id": response.ID}
			}
		}()
	}
	for i := range requests {
		expectedID := accounts[i].ConnectionID + "." + accounts[i].ID
		if existingIDs[expectedID] {
			results[i] = map[string]any{"input": accounts[i], "status": "skipped_existing", "id": expectedID}
			continue
		}
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	failed, skipped := 0, 0
	for _, result := range results {
		if result["status"] == "failed" {
			failed++
		} else if result["status"] == "skipped_existing" {
			skipped++
		}
	}
	status := "success"
	if failed > 0 {
		status = "partial_failure"
	}
	created := len(accounts) - failed - skipped
	envelope := mutationEnvelope{SchemaVersion: "1", Status: status, Operation: operation, Organization: orgID, Result: map[string]any{"created": created, "skipped": skipped, "failed": failed, "concurrency": workerCount, "accounts": results, "nextCommand": "bitwave org accounting status --json"}}
	if failed > 0 {
		_ = writeJSON(cmd.OutOrStdout(), envelope)
		return fmt.Errorf("chart import: %d created, %d skipped, %d failed", created, skipped, failed)
	}
	return outputMutation(cmd, f.jsonOutput, envelope, fmt.Sprintf("chart of accounts: %d created, %d skipped\n", created, skipped))
}

func createChartAccountWithRetry(cmd *cobra.Command, client *orgreports.Client, orgID string, request orgreports.CreateChartAccountInput) (*orgreports.CreateChartAccountResponse, error) {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		response, err := client.CreateChartAccount(cmd.Context(), orgID, request)
		if err == nil {
			return response, nil
		}
		lastErr = err
		var apiError *apierr.Error
		if !errors.As(err, &apiError) || apiError.Status != http.StatusTooManyRequests || attempt == 4 {
			return nil, err
		}
		delay := time.Duration(1<<attempt) * time.Second
		select {
		case <-cmd.Context().Done():
			return nil, cmd.Context().Err()
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}

func validateChartAccount(input chartAccountInput) error {
	if strings.TrimSpace(input.ConnectionID) == "" {
		return errors.New("accounting connection ID is required")
	}
	if strings.TrimSpace(input.ID) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(input.Name) == "" {
		return errors.New("name is required")
	}
	if strings.EqualFold(strings.TrimSpace(input.Name), "Digital Assets") {
		return errors.New("Digital Assets is provided automatically by Bitwave; do not create a duplicate account")
	}
	if !stringIn(strings.ToLower(strings.TrimSpace(input.Type)), chartAccountTypes...) {
		return fmt.Errorf("type must be one of: %s", strings.Join(chartAccountTypes, ", "))
	}
	return nil
}

func accountRequest(input chartAccountInput) orgreports.CreateChartAccountInput {
	return orgreports.CreateChartAccountInput{ConnectionID: strings.TrimSpace(input.ConnectionID), Source: "manual", ID: strings.TrimSpace(input.ID), Name: strings.TrimSpace(input.Name), Type: strings.ToLower(strings.TrimSpace(input.Type)), Code: strings.TrimSpace(input.Code), Description: strings.TrimSpace(input.Description)}
}

func loadChartAccounts(path string, stdin io.Reader) ([]chartAccountInput, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("--input is required")
	}
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read chart input: %w", err)
	}
	var accounts []chartAccountInput
	if err := json.Unmarshal(data, &accounts); err == nil {
		return accounts, nil
	}
	var wrapper struct {
		Accounts []chartAccountInput `json:"accounts"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("decode chart input: %w", err)
	}
	return wrapper.Accounts, nil
}

func accountingClient(explicitOrg string) (string, *orgreports.Client, error) {
	orgID, err := resolveReportOrg(explicitOrg)
	if err != nil {
		return "", nil, err
	}
	token, err := makeOrgTokenResolver(orgID)()
	if err != nil {
		return "", nil, fmt.Errorf("resolve organization token: %w", err)
	}
	return orgID, orgreports.New(resolveCoreBaseURL(), func() (string, error) { return token, nil }), nil
}
