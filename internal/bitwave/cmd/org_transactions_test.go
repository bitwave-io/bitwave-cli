package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func TestIgnoreDryRunDoesNotRequireConfirmation(t *testing.T) {
	cmd := newTransactionStateCmd("ignore", "ignore")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"txn-1", "txn-1", "txn-2", "--org", "org-1", "--dry-run", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var envelope mutationEnvelope
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("output = %s err=%v", out.String(), err)
	}
	if envelope.Status != "preview" || envelope.Operation != "ignore" || !envelope.DryRun {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestBulkCategorizationTypedBody(t *testing.T) {
	body, err := bulkCategorizationBody(bulkCategorizeFlags{
		kind: "multivalue", transactionIDs: []string{"txn-1", "txn-1"}, accountingConnectionID: "ac-1",
		feeContactID: "contact-fee", feeCategoryID: "category-fee",
		sendContactID: "contact-send", sendCategoryID: "category-send",
		receiveContactID: "contact-receive", receiveCategoryID: "category-receive", overwrite: true,
	}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Categorization struct {
			AccountingConnectionID string `json:"accountingConnectionId"`
			Multivalue             struct {
				TxnIDs []string `json:"txnIds"`
			} `json:"multivalue"`
		} `json:"categorization"`
		Options struct {
			Overwrite bool `json:"overwriteExistingCategorization"`
		} `json:"options"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Categorization.AccountingConnectionID != "ac-1" || len(decoded.Categorization.Multivalue.TxnIDs) != 1 || !decoded.Options.Overwrite {
		t.Fatalf("body = %s", body)
	}
}

func TestSingleCategorizationValidation(t *testing.T) {
	if err := validateSingleCategorization(json.RawMessage(`{"type":"trade","categorizationMethod":1,"accountingConnectionId":"ac-1","exchangeRates":[],"exchangeRateVersion":0}`)); err != nil {
		t.Fatal(err)
	}
	if err := validateSingleCategorization(json.RawMessage(`{"type":"trade","categorizationMethod":1,"exchangeRates":[],"exchangeRateVersion":0}`)); err == nil {
		t.Fatal("expected missing connection error")
	}
	if err := validateSingleCategorization(json.RawMessage(`{"type":"made-up","categorizationMethod":1,"accountingConnectionId":"ac-1","exchangeRates":[],"exchangeRateVersion":0}`)); err == nil {
		t.Fatal("expected unsupported type error")
	}
}

func TestCategorizationOptionFilters(t *testing.T) {
	categories := filterCategories([]orgreports.Category{
		{ID: "cat-1", Name: "Staking Revenue", Code: "4000", Enabled: true, AccountingConnectionID: "ac-1"},
		{ID: "cat-2", Name: "Staking Fees", Enabled: false, AccountingConnectionID: "ac-1"},
		{ID: "cat-3", Name: "Staking Revenue", Enabled: true, AccountingConnectionID: "ac-2"},
	}, "staking", "ac-1", false)
	if len(categories) != 1 || categories[0].ID != "cat-1" {
		t.Fatalf("categories = %#v", categories)
	}
	contacts := filterContacts([]orgreports.Contact{
		{ID: "contact-1", Name: "Validator", Enabled: true, AccountingConnectionID: "ac-1"},
		{ID: "contact-2", Name: "Validator", Enabled: true, AccountingConnectionID: "ac-2"},
	}, "valid", "ac-2", false)
	if len(contacts) != 1 || contacts[0].ID != "contact-2" {
		t.Fatalf("contacts = %#v", contacts)
	}
}
