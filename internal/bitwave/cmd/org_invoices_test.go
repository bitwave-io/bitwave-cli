package cmd

import (
	"encoding/json"
	"testing"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func TestFilterInvoicesMatchesUIDropdownEligibility(t *testing.T) {
	items := []orgreports.Invoice{
		{ID: "ac-1.invoice-1", AccountingConnectionID: "ac-1", Type: "Receiving", Status: "AwaitingPayment", DueAmount: json.Number("12.34")},
		{ID: "ac-1.invoice-2", AccountingConnectionID: "ac-1", Type: "Receiving", Status: "Paid", DueAmount: json.Number("0")},
		{ID: "ac-1.bill-1", AccountingConnectionID: "ac-1", Type: "Paying", Status: "AwaitingPayment", DueAmount: json.Number("50")},
		{ID: "ac-2.invoice-1", AccountingConnectionID: "ac-2", Type: "Receiving", Status: "AwaitingPayment", DueAmount: json.Number("10")},
	}
	result := filterInvoices(items, "ac-1", "Receiving", "AwaitingPayment")
	if len(result) != 1 || result[0].ID != "ac-1.invoice-1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestInvoiceDirectionAndStatusAliases(t *testing.T) {
	if value, err := invoiceTypeForDirection("deposit"); err != nil || value != "Receiving" {
		t.Fatalf("direction = %q err=%v", value, err)
	}
	if value, err := invoiceStatusValue("unpaid"); err != nil || value != "AwaitingPayment" {
		t.Fatalf("status = %q err=%v", value, err)
	}
	if _, err := invoiceTypeForDirection("sideways"); err == nil {
		t.Fatal("expected invalid direction")
	}
}

func TestInvoiceCommandRequiresContact(t *testing.T) {
	cmd := newOrgInvoiceListCmd()
	cmd.SetArgs([]string{"--org", "org-1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected --contact validation error")
	}
}
