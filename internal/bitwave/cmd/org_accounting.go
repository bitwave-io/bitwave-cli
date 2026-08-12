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

const implicitManualConnectionID = "Manual"

func withImplicitManualConnection(connections []orgreports.AccountingConnection) []orgreports.AccountingConnection {
	for _, connection := range connections {
		if strings.EqualFold(strings.TrimSpace(connection.ID), implicitManualConnectionID) {
			return connections
		}
	}
	result := append([]orgreports.AccountingConnection(nil), connections...)
	return append(result, orgreports.AccountingConnection{ID: implicitManualConnectionID, Name: "Bitwave", Type: "manual"})
}

type accountingReadiness struct {
	AccountingConnectionReady   bool                              `json:"accountingConnectionReady"`
	DigitalAssetsAccountPresent bool                              `json:"digitalAssetsAccountPresent"`
	ReadyForRules               bool                              `json:"readyForRules"`
	Decision                    string                            `json:"decision"`
	InteractionRequired         bool                              `json:"interactionRequired"`
	Connections                 []orgreports.AccountingConnection `json:"connections"`
	ConnectionCount             int                               `json:"connectionCount"`
	AdditionalCategoryCount     int                               `json:"additionalCategoryCount"`
	ContactCount                int                               `json:"contactCount"`
	Starter                     accountingStarterPolicy           `json:"starter"`
	Prompt                      map[string]any                    `json:"prompt,omitempty"`
	NextCommands                []string                          `json:"nextCommands"`
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
	AdvisoryOnly      bool                            `json:"advisoryOnly"`
	Guardrails        []string                        `json:"guardrails"`
}

func newOrgAccountingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accounting",
		Short: "Prepare an accounting connection and client-specific accounts before categorization",
		Long: `Inspect accounting readiness before creating categorization rules.

The CLI should use Bitwave's implicit Manual connection (stable ID: Manual),
which supplies Digital Assets and Crypto Fees, or an existing external
accounting connection. It must not create a generated manual connection during
normal onboarding. Other connection IDs do not inherit Manual's built-ins.
External provider authorization
remains in the Bitwave web app; manual setup and imports are available here.`,
	}
	cmd.AddCommand(newOrgAccountingStatusCmd(), newOrgAccountingConnectionsCmd(), newOrgAccountingManualCmd(), newOrgAccountingAccountsCmd(), newOrgAccountingContactsCmd(), newOrgAccountingStarterCmd())
	return cmd
}

func newOrgAccountingContactsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "contacts", Short: "List or create Bitwave categorization contacts"}
	cmd.AddCommand(newOrgAccountingContactsListCmd(), newOrgAccountingContactCreateCmd())
	return cmd
}

func newOrgAccountingContactsListCmd() *cobra.Command {
	var orgID, connectionID, query string
	var limit int
	cmd := &cobra.Command{
		Use: "list", Short: "List a bounded set of contacts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > 500 {
				return errors.New("--limit must be between 1 and 500")
			}
			resolvedOrg, client, err := accountingClient(orgID)
			if err != nil {
				return err
			}
			contacts, err := client.Contacts(cmd.Context(), resolvedOrg)
			if err != nil {
				return fmt.Errorf("list contacts: %w", err)
			}
			contacts = filterContacts(contacts, query, connectionID, false)
			total := len(contacts)
			if len(contacts) > limit {
				contacts = contacts[:limit]
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "contacts": contacts, "total": total, "truncated": total > len(contacts)})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().StringVar(&connectionID, "accounting-connection", "", "Only contacts belonging to this connection ID")
	cmd.Flags().StringVar(&query, "query", "", "Case-insensitive name or ID substring")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum contacts to return")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgAccountingContactCreateCmd() *cobra.Command {
	var f transactionMutationFlags
	var input orgreports.CreateContactInput
	cmd := &cobra.Command{
		Use: "create", Short: "Create one categorization contact",
		RunE: func(cmd *cobra.Command, _ []string) error {
			operation := "create-contact"
			input.ConnectionID = strings.TrimSpace(input.ConnectionID)
			input.RemoteID = strings.TrimSpace(input.RemoteID)
			input.Name = strings.TrimSpace(input.Name)
			input.Type = strings.TrimSpace(input.Type)
			if input.ConnectionID == "" || input.RemoteID == "" || input.Name == "" {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("--accounting-connection, --id, and --name are required"))
			}
			if !strings.EqualFold(input.Type, "Customer") && !strings.EqualFold(input.Type, "Vendor") {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("--type must be Customer or Vendor"))
			}
			if strings.EqualFold(input.Type, "customer") {
				input.Type = "Customer"
			} else {
				input.Type = "Vendor"
			}
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: input})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to change the organization without --yes (use --dry-run to preview)"))
			}
			_, client, err := accountingClient(orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			contacts, err := client.Contacts(cmd.Context(), orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("check existing contacts: %w", err))
			}
			for _, contact := range contacts {
				if strings.EqualFold(contact.AccountingConnectionID, input.ConnectionID) && (strings.EqualFold(contact.RemoteID, input.RemoteID) || strings.EqualFold(contact.Name, input.Name)) {
					return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: map[string]any{"status": "skipped_existing", "contact": contact}})
				}
			}
			id, err := client.CreateContact(cmd.Context(), orgID, input)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: map[string]any{"status": "created", "id": id, "contact": input}})
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringVar(&input.ConnectionID, "accounting-connection", "", "Accounting connection ID")
	cmd.Flags().StringVar(&input.RemoteID, "id", "", "Stable remote/contact ID")
	cmd.Flags().StringVar(&input.Name, "name", "", "Contact name")
	cmd.Flags().StringVar(&input.Type, "type", "", "Contact type: Customer or Vendor")
	return cmd
}

func starterPolicy(connectionID string) accountingStarterPolicy {
	automaticAccounts := []string{}
	if strings.EqualFold(strings.TrimSpace(connectionID), implicitManualConnectionID) {
		automaticAccounts = []string{"Digital Assets", "Crypto Fees"}
	}
	return accountingStarterPolicy{
		AutomaticAccounts: automaticAccounts,
		AdvisoryOnly:      true,
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
			"Use Digital Assets and Crypto Fees from the implicit Manual connection only; other generated or external connection IDs do not inherit them.",
			"Warn before creating token/network/protocol-specific asset accounts; proceed if the user requests it.",
			"Treat starter revenue and expense resources as fallbacks, not inferred accounting policy.",
			"Trade rules use the Gas Fees contact with no fee category and autoCategorizeFee=false.",
			"Recommend that the user specify or approve every additional category and contact; guidance never blocks execution.",
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
	connections = withImplicitManualConnection(connections)
	active := make([]orgreports.AccountingConnection, 0, len(connections))
	activeIDs := map[string]bool{}
	for _, connection := range connections {
		if !connection.Disabled {
			active = append(active, connection)
			activeIDs[connection.ID] = true
		}
	}
	availableAccounts := 0
	digitalAssetsPresent := activeIDs[implicitManualConnectionID]
	for _, category := range categories {
		if category.Enabled && activeIDs[category.AccountingConnectionID] {
			availableAccounts++
			if strings.EqualFold(strings.TrimSpace(category.Name), "Digital Assets") {
				digitalAssetsPresent = true
			}
		}
	}
	availableContacts := 0
	for _, contact := range contacts {
		if contact.Enabled && activeIDs[contact.AccountingConnectionID] {
			availableContacts++
		}
	}
	readiness := accountingReadiness{
		AccountingConnectionReady: len(active) > 0, DigitalAssetsAccountPresent: digitalAssetsPresent,
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
		readiness.Decision = "accounting_connection_missing"
		readiness.InteractionRequired = true
		readiness.Prompt = map[string]any{
			"question": "No active accounting connection was found. The automatically provisioned manual setup may be missing; verify organization provisioning or connect the client's external accounting system.",
			"choices": []map[string]string{
				{"id": "connect_external", "label": "Connect accounting system", "next": "Open Accounting Connections in the Bitwave web app to authorize the provider, then rerun status."},
				{"id": "verify_manual", "label": "Verify manual setup", "next": "Run `bitwave org accounting connections list --json`; if absent, contact Bitwave rather than creating a duplicate connection."},
			},
		}
		readiness.NextCommands = []string{"bitwave org accounting connections list --json", "bitwave org accounting status --json"}
	case availableAccounts == 0 && availableContacts == 0:
		readiness.Decision = "client_categories_and_contacts_needed"
		readiness.InteractionRequired = true
		readiness.Prompt = map[string]any{
			"question": "This accounting connection has no categories or contacts. Which Digital Assets mapping and client-specific categorization accounts and contacts should be added?",
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
		readiness.Prompt = map[string]any{"question": "Which client-specific categories should be added, and does this connection need a Digital Assets account mapping?"}
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
			connections = withImplicitManualConnection(connections)
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "connections": connections})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgAccountingManualCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "manual", Short: "Discover the automatically provisioned manual accounting connection"}
	cmd.AddCommand(newOrgAccountingManualCreateCmd())
	return cmd
}

func newOrgAccountingManualCreateCmd() *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{
		Use: "use", Aliases: []string{"create"}, Short: "Select the existing manual connection without creating another",
		RunE: func(cmd *cobra.Command, _ []string) error {
			operation := "use-manual-accounting-connection"
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			_, client, err := accountingClient(orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			connections, err := client.AccountingConnections(cmd.Context(), orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("check accounting connections: %w", err))
			}
			connections = withImplicitManualConnection(connections)
			for _, connection := range connections {
				if !connection.Disabled && (strings.Contains(strings.ToLower(connection.Type), "manual") || strings.EqualFold(connection.Name, "manual")) {
					status := "success"
					if f.dryRun {
						status = "preview"
					}
					envelope := mutationEnvelope{SchemaVersion: "1", Status: status, Operation: operation, Organization: orgID, DryRun: f.dryRun, Result: map[string]any{"status": "existing_manual_selected", "connectionId": connection.ID, "connection": connection, "nextCommand": "bitwave org accounting status --json"}}
					return outputMutation(cmd, f.jsonOutput, envelope, "using existing manual accounting connection: "+connection.ID+"\n")
				}
			}
			return mutationError(cmd, operation, f.jsonOutput, errors.New("the automatically provisioned manual accounting connection was not found; do not create a second connection via the CLI—verify organization provisioning or contact Bitwave"))
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
	warnings := make([]string, 0)
	for _, account := range accounts {
		requests = append(requests, accountRequest(account))
		warnings = append(warnings, chartAccountAdvisories(account)...)
	}
	preview := map[string]any{"method": "POST", "path": "/org/" + orgID + "/categories", "requests": requests}
	if f.dryRun {
		return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: preview, Warnings: warnings})
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
	connections = withImplicitManualConnection(connections)
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
	envelope := mutationEnvelope{SchemaVersion: "1", Status: status, Operation: operation, Organization: orgID, Result: map[string]any{"created": created, "skipped": skipped, "failed": failed, "concurrency": workerCount, "accounts": results, "nextCommand": "bitwave org accounting status --json"}, Warnings: warnings}
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
	if !stringIn(strings.ToLower(strings.TrimSpace(input.Type)), chartAccountTypes...) {
		return fmt.Errorf("type must be one of: %s", strings.Join(chartAccountTypes, ", "))
	}
	return nil
}

func chartAccountAdvisories(input chartAccountInput) []string {
	if strings.EqualFold(strings.TrimSpace(input.Name), "Digital Assets") {
		return []string{"Confirm whether this accounting connection already contains or maps a Digital Assets account; creating another may duplicate the client's chart. The CLI will still submit it."}
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
