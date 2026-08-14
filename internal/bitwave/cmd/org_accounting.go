package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

var chartAccountTypes = []string{"asset", "bank", "equity", "expense", "liability", "other", "revenue"}

type accountingReadiness struct {
	Connections       []orgreports.AccountingConnection `json:"connections"`
	ConnectionCount   int                               `json:"connectionCount"`
	ChartAccountCount int                               `json:"chartAccountCount"`
}

type chartAccountInput struct {
	ConnectionID string `json:"connectionId"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Code         string `json:"code"`
	Description  string `json:"description"`
}

func newOrgAccountingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accounting",
		Short: "Inspect and manage Bitwave accounting connections and chart accounts",
	}
	cmd.AddCommand(newOrgAccountingStatusCmd(), newOrgAccountingConnectionsCmd(), newOrgAccountingManualCmd(), newOrgAccountingAccountsCmd(), newOrgAccountingContactsCmd())
	return cmd
}

func newOrgAccountingContactsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "contacts", Short: "List, create, and update Bitwave categorization contacts"}
	cmd.AddCommand(
		newOrgAccountingContactsListCmd(), newOrgAccountingContactCreateCmd(), newOrgAccountingContactUpdateCmd(),
		newOrgAccountingContactToggleCmd("enable", true), newOrgAccountingContactToggleCmd("disable", false),
		newOrgAccountingContactsDisableAllCmd(),
	)
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
			query = strings.ToLower(strings.TrimSpace(query))
			connectionID = strings.TrimSpace(connectionID)
			filtered := make([]orgreports.Contact, 0, len(contacts))
			for _, contact := range contacts {
				if connectionID != "" && contact.AccountingConnectionID != connectionID {
					continue
				}
				haystack := strings.ToLower(strings.Join([]string{contact.ID, contact.RemoteID, contact.Name, contact.Type}, " "))
				if query != "" && !strings.Contains(haystack, query) {
					continue
				}
				filtered = append(filtered, contact)
			}
			total := len(filtered)
			if len(filtered) > limit {
				filtered = filtered[:limit]
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "contacts": filtered, "total": total, "truncated": total > len(filtered)})
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
		Use: "create", Short: "Create one Bitwave categorization contact",
		RunE: func(cmd *cobra.Command, _ []string) error {
			operation := "create-contact"
			input.ConnectionID = strings.TrimSpace(input.ConnectionID)
			input.RemoteID = strings.TrimSpace(input.RemoteID)
			input.Name = strings.TrimSpace(input.Name)
			input.Type = strings.TrimSpace(input.Type)
			input.FirstName = strings.TrimSpace(input.FirstName)
			input.LastName = strings.TrimSpace(input.LastName)
			input.EmailAddress = strings.TrimSpace(input.EmailAddress)
			if input.ConnectionID == "" || input.Name == "" {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("--accounting-connection and --name are required"))
			}
			if !strings.EqualFold(input.Type, "None") && !strings.EqualFold(input.Type, "Customer") && !strings.EqualFold(input.Type, "Vendor") {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("--type must be None, Customer, or Vendor"))
			}
			if strings.EqualFold(input.Type, "customer") {
				input.Type = "Customer"
			} else if strings.EqualFold(input.Type, "vendor") {
				input.Type = "Vendor"
			} else {
				input.Type = "None"
			}
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			if f.dryRun {
				return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: input})
			}
			if !f.yes {
				return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to change the organization without --yes"))
			}
			_, client, err := accountingClient(orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			id, err := client.CreateContact(cmd.Context(), orgID, input)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: map[string]any{"id": id, "contact": input}})
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringVar(&input.ConnectionID, "accounting-connection", "", "Accounting connection ID")
	cmd.Flags().StringVar(&input.RemoteID, "id", "", "Optional remote contact ID")
	cmd.Flags().StringVar(&input.Name, "name", "", "Contact name")
	cmd.Flags().StringVar(&input.Type, "type", "None", "Contact type: None, Customer, or Vendor")
	cmd.Flags().StringVar(&input.FirstName, "first-name", "", "Optional first name")
	cmd.Flags().StringVar(&input.LastName, "last-name", "", "Optional last name")
	cmd.Flags().StringVar(&input.EmailAddress, "email", "", "Optional email address")
	return cmd
}

func newOrgAccountingContactUpdateCmd() *cobra.Command {
	var f transactionMutationFlags
	var inputPath string
	cmd := &cobra.Command{
		Use: "update CONTACT_ID", Short: "Update a contact using Bitwave's complete UpdateContactInput", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := readJSONObject(inputPath, cmd.InOrStdin())
			if err != nil {
				return mutationError(cmd, "update-contact", f.jsonOutput, err)
			}
			body, err = contactUpdateBody(args[0], body)
			if err != nil {
				return mutationError(cmd, "update-contact", f.jsonOutput, err)
			}
			return runContactUpdate(cmd, f, "update-contact", body)
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringVarP(&inputPath, "input", "i", "", "UpdateContactInput JSON file, or - for stdin (required)")
	return cmd
}

func newOrgAccountingContactToggleCmd(name string, enabled bool) *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{
		Use: name + " CONTACT_ID", Short: strings.ToUpper(name[:1]) + name[1:] + " a contact", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]any{"id": args[0], "enabled": enabled})
			return runContactUpdate(cmd, f, name+"-contact", body)
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}

func runContactUpdate(cmd *cobra.Command, f transactionMutationFlags, operation string, body json.RawMessage) error {
	orgID, err := resolveReportOrg(f.orgID)
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, err)
	}
	if f.dryRun {
		return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: body})
	}
	if !f.yes {
		return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to change the organization without --yes"))
	}
	_, client, err := accountingClient(orgID)
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, err)
	}
	result, err := client.UpdateContact(cmd.Context(), orgID, body)
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, err)
	}
	return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: result})
}

func contactUpdateBody(contactID string, body json.RawMessage) (json.RawMessage, error) {
	var input map[string]any
	if err := json.Unmarshal(body, &input); err != nil {
		return nil, err
	}
	if existing, ok := input["id"].(string); ok && existing != "" && existing != contactID {
		return nil, fmt.Errorf("input contact id %q does not match argument %q", existing, contactID)
	}
	input["id"] = contactID
	return json.Marshal(input)
}

func newOrgAccountingContactsDisableAllCmd() *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{Use: "disable-all", Short: "Disable every organization contact", RunE: func(cmd *cobra.Command, _ []string) error {
		return runDisableAllAccountingResources(cmd, f, "disable-contacts", "contacts")
	}}
	addMutationFlags(cmd, &f)
	return cmd
}

func newOrgAccountingStatusCmd() *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Return accounting-connection and chart-account counts",
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
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": "1", "organization": resolvedOrg,
				"readiness": buildAccountingReadiness(connections, categories),
			})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func buildAccountingReadiness(connections []orgreports.AccountingConnection, categories []orgreports.Category) accountingReadiness {
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
	readiness := accountingReadiness{
		Connections: active, ConnectionCount: len(active), ChartAccountCount: availableAccounts,
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
		Use: "create", Short: "Create a manual accounting connection and its default Bitwave accounts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			operation := "create-manual-accounting-connection"
			orgID, err := resolveReportOrg(f.orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, err)
			}
			preview := map[string]any{"method": "POST", "path": "/orgs/" + orgID + "/connections/manual"}
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
					envelope := mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: map[string]any{"status": "skipped_existing", "connectionId": connection.ID, "nextCommand": "bitwave org accounting status --json"}}
					return outputMutation(cmd, f.jsonOutput, envelope, "manual accounting connection already exists: "+connection.ID+"\n")
				}
			}
			response, err := client.CreateManualAccountingConnection(cmd.Context(), orgID)
			if err != nil {
				return mutationError(cmd, operation, f.jsonOutput, fmt.Errorf("create manual accounting connection: %w", err))
			}
			envelope := mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: map[string]any{"connectionId": response.ConnectionID, "nextCommand": "bitwave org accounting status --json"}}
			return outputMutation(cmd, f.jsonOutput, envelope, "created manual accounting connection "+response.ConnectionID+"\n")
		},
	}
	addMutationFlags(cmd, &f)
	return cmd
}

func newOrgAccountingAccountsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "accounts", Aliases: []string{"categories"}, Short: "List, create, import, enable, or disable Bitwave categories/accounts"}
	cmd.AddCommand(
		newOrgAccountingAccountsListCmd(), newOrgAccountingAccountCreateCmd(), newOrgAccountingAccountsImportCmd(),
		newOrgAccountingCategoryToggleCmd("enable", true), newOrgAccountingCategoryToggleCmd("disable", false),
		newOrgAccountingCategoriesDisableAllCmd(),
	)
	return cmd
}

func newOrgAccountingCategoryToggleCmd(name string, enabled bool) *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{Use: name + " CATEGORY_ID", Short: strings.ToUpper(name[:1]) + name[1:] + " a category/account", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		operation := name + "-category"
		orgID, err := resolveReportOrg(f.orgID)
		if err != nil {
			return mutationError(cmd, operation, f.jsonOutput, err)
		}
		request := map[string]any{"id": args[0], "enabled": enabled}
		if f.dryRun {
			return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: request})
		}
		if !f.yes {
			return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to change the organization without --yes"))
		}
		_, client, err := accountingClient(orgID)
		if err != nil {
			return mutationError(cmd, operation, f.jsonOutput, err)
		}
		result, err := client.UpdateCategoryEnabled(cmd.Context(), orgID, args[0], enabled)
		if err != nil {
			return mutationError(cmd, operation, f.jsonOutput, err)
		}
		return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: result})
	}}
	addMutationFlags(cmd, &f)
	return cmd
}

func newOrgAccountingCategoriesDisableAllCmd() *cobra.Command {
	var f transactionMutationFlags
	cmd := &cobra.Command{Use: "disable-all", Short: "Disable every organization category/account", RunE: func(cmd *cobra.Command, _ []string) error {
		return runDisableAllAccountingResources(cmd, f, "disable-categories", "categories")
	}}
	addMutationFlags(cmd, &f)
	return cmd
}

func runDisableAllAccountingResources(cmd *cobra.Command, f transactionMutationFlags, operation, resource string) error {
	orgID, err := resolveReportOrg(f.orgID)
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, err)
	}
	request := map[string]any{"resource": resource, "all": true}
	if f.dryRun {
		return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "preview", Operation: operation, Organization: orgID, DryRun: true, Request: request})
	}
	if !f.yes {
		return mutationError(cmd, operation, f.jsonOutput, errors.New("refusing to disable all resources without --yes"))
	}
	_, client, err := accountingClient(orgID)
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, err)
	}
	if resource == "contacts" {
		err = client.DisableContacts(cmd.Context(), orgID)
	} else {
		err = client.DisableCategories(cmd.Context(), orgID)
	}
	if err != nil {
		return mutationError(cmd, operation, f.jsonOutput, err)
	}
	return writeJSON(cmd.OutOrStdout(), mutationEnvelope{SchemaVersion: "1", Status: "success", Operation: operation, Organization: orgID, Result: request})
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
			return runCreateChartAccounts(cmd, []chartAccountInput{input}, f)
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
	cmd := &cobra.Command{
		Use: "import", Short: "Import a JSON array of accounts into a manual Bitwave chart",
		RunE: func(cmd *cobra.Command, _ []string) error {
			accounts, err := loadChartAccounts(inputPath, cmd.InOrStdin())
			if err != nil {
				return mutationError(cmd, "import-chart-of-accounts", f.jsonOutput, err)
			}
			return runCreateChartAccounts(cmd, accounts, f)
		},
	}
	addMutationFlags(cmd, &f)
	cmd.Flags().StringVarP(&inputPath, "input", "i", "", "Accounts JSON file, or - for stdin (required)")
	return cmd
}

func runCreateChartAccounts(cmd *cobra.Command, accounts []chartAccountInput, f transactionMutationFlags) error {
	operation := "import-chart-of-accounts"
	if len(accounts) == 0 {
		return mutationError(cmd, operation, f.jsonOutput, errors.New("at least one chart account is required"))
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
	jobs := make(chan int)
	workerCount := min(8, len(requests))
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				response, createErr := client.CreateChartAccount(cmd.Context(), orgID, requests[i])
				if createErr != nil {
					results[i] = map[string]any{"input": accounts[i], "status": "failed", "error": createErr.Error()}
					continue
				}
				results[i] = map[string]any{"input": accounts[i], "status": "created", "id": response.ID}
			}
		}()
	}
	for i := range requests {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	failed := 0
	for _, result := range results {
		if result["status"] == "failed" {
			failed++
		}
	}
	status := "success"
	if failed > 0 {
		status = "partial_failure"
	}
	envelope := mutationEnvelope{SchemaVersion: "1", Status: status, Operation: operation, Organization: orgID, Result: map[string]any{"created": len(accounts) - failed, "failed": failed, "concurrency": workerCount, "accounts": results, "nextCommand": "bitwave org accounting status --json"}}
	if failed > 0 {
		_ = writeJSON(cmd.OutOrStdout(), envelope)
		return fmt.Errorf("chart import: %d created, %d failed", len(accounts)-failed, failed)
	}
	return outputMutation(cmd, f.jsonOutput, envelope, fmt.Sprintf("chart of accounts: %d created\n", len(accounts)))
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
	accountType := strings.ToLower(strings.TrimSpace(input.Type))
	validType := false
	for _, allowed := range chartAccountTypes {
		if accountType == allowed {
			validType = true
			break
		}
	}
	if !validType {
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
