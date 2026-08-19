package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func newOrgInvoicesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "invoice",
		Aliases: []string{"invoices", "bill", "bills"},
		Short:   "Find Bitwave invoices and bills for transaction categorization",
		Long: `Find invoices and bills imported from an accounting connection.

Invoice discovery is contact-scoped, matching the Bitwave transaction UI: select
an accounting connection and contact first, then list the eligible AR invoices
for an inflow or AP bills for an outflow. Use the returned stable IDs in an
invoice-v2 transaction categorization payload.`,
	}
	cmd.AddCommand(newOrgInvoiceListCmd(), newOrgInvoiceGetCmd())
	return cmd
}

func newOrgInvoiceListCmd() *cobra.Command {
	var orgID, contactID, connectionID, direction, status, pageToken string
	var limit int
	var includeDisabled bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List contact-scoped invoices or bills eligible for categorization",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			contactID = strings.TrimSpace(contactID)
			if contactID == "" {
				return errors.New("--contact is required; select a contact before loading its invoices")
			}
			if limit <= 0 || limit > 100 {
				return errors.New("--limit must be between 1 and 100")
			}
			invoiceType, err := invoiceTypeForDirection(direction)
			if err != nil {
				return err
			}
			invoiceStatus, err := invoiceStatusValue(status)
			if err != nil {
				return err
			}
			connectionID = strings.TrimSpace(connectionID)
			if connectionID != "" {
				contactConnectionID := invoiceResourceConnectionID(contactID)
				if contactConnectionID != "" && contactConnectionID != connectionID {
					return fmt.Errorf("contact %q belongs to accounting connection %q, not %q", contactID, contactConnectionID, connectionID)
				}
			}

			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			page, err := client.Invoices(cmd.Context(), resolvedOrg, orgreports.InvoiceListInput{
				ContactID: contactID, PageToken: strings.TrimSpace(pageToken), PageSize: limit, IncludeDisabled: includeDisabled,
			})
			if err != nil {
				return fmt.Errorf("list invoices for contact %s: %w", contactID, err)
			}
			invoices := filterInvoices(page.Records, connectionID, invoiceType, invoiceStatus)
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"schemaVersion": "1", "organization": resolvedOrg, "contactId": contactID,
				"accountingConnectionId": connectionID, "direction": normalizedInvoiceDirection(direction), "status": normalizedInvoiceStatus(status),
				"count": len(invoices), "invoices": invoices, "previousPageToken": page.PreviousPageToken, "nextPageToken": page.NextPageToken,
				"warning": "Only invoices for the selected contact are returned. Match inflows to Receiving invoices and outflows to Paying bills.",
			})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().StringVar(&contactID, "contact", "", "Exact Bitwave contact ID (required)")
	cmd.Flags().StringVar(&connectionID, "accounting-connection", "", "Expected accounting connection ID")
	cmd.Flags().StringVar(&direction, "direction", "all", "Transaction direction: inflow, outflow, or all")
	cmd.Flags().StringVar(&status, "status", "awaiting-payment", "Invoice status: awaiting-payment, paid, draft, other, or all")
	cmd.Flags().StringVar(&pageToken, "next-token", "", "Opaque next-page token from the previous response")
	cmd.Flags().IntVar(&limit, "limit", 25, "Maximum remote records to inspect (1-100)")
	cmd.Flags().BoolVar(&includeDisabled, "include-disabled", false, "Include disabled invoices")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func newOrgInvoiceGetCmd() *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use:   "get INVOICE_ID",
		Short: "Get one imported invoice or bill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedOrg, err := resolveReportOrg(orgID)
			if err != nil {
				return err
			}
			client := orgreports.New(resolveCoreBaseURL(), makeOrgTokenResolver(resolvedOrg))
			invoice, err := client.Invoice(cmd.Context(), resolvedOrg, strings.TrimSpace(args[0]))
			if err != nil {
				return fmt.Errorf("get invoice %s: %w", args[0], err)
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{"schemaVersion": "1", "organization": resolvedOrg, "invoice": invoice})
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID override")
	cmd.Flags().Bool("json", true, "Emit machine-readable JSON (the only supported format)")
	return cmd
}

func filterInvoices(items []orgreports.Invoice, connectionID, invoiceType, status string) []orgreports.Invoice {
	result := make([]orgreports.Invoice, 0, len(items))
	for _, item := range items {
		if connectionID != "" && item.AccountingConnectionID != connectionID {
			continue
		}
		if invoiceType != "" && !strings.EqualFold(item.Type, invoiceType) {
			continue
		}
		if status != "" && !strings.EqualFold(item.Status, status) {
			continue
		}
		if status == "AwaitingPayment" {
			due, err := item.DueAmount.Float64()
			if err != nil || due <= 0 {
				continue
			}
		}
		result = append(result, item)
	}
	return result
}

func invoiceTypeForDirection(value string) (string, error) {
	switch normalizedInvoiceDirection(value) {
	case "inflow":
		return "Receiving", nil
	case "outflow":
		return "Paying", nil
	case "all":
		return "", nil
	default:
		return "", errors.New("--direction must be inflow, outflow, or all")
	}
}

func normalizedInvoiceDirection(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "inflow", "receive", "receiving", "deposit":
		return "inflow"
	case "outflow", "send", "paying", "withdraw":
		return "outflow"
	case "", "all":
		return "all"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func invoiceStatusValue(value string) (string, error) {
	switch normalizedInvoiceStatus(value) {
	case "awaiting-payment":
		return "AwaitingPayment", nil
	case "paid":
		return "Paid", nil
	case "draft":
		return "Draft", nil
	case "other":
		return "Other", nil
	case "all":
		return "", nil
	default:
		return "", errors.New("--status must be awaiting-payment, paid, draft, other, or all")
	}
}

func normalizedInvoiceStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	switch value {
	case "", "awaitingpayment", "awaiting-payment", "unpaid":
		return "awaiting-payment"
	default:
		return value
	}
}

func invoiceResourceConnectionID(id string) string {
	if index := strings.IndexByte(id, '.'); index > 0 {
		return id[:index]
	}
	return ""
}
