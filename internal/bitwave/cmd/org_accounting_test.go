package cmd

import (
	"strings"
	"testing"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func TestAccountingReadinessPromptsForSetup(t *testing.T) {
	readiness := buildAccountingReadiness(nil, nil, nil)
	if readiness.ReadyForRules || !readiness.InteractionRequired || readiness.Decision != "choose_accounting_setup" {
		t.Fatalf("readiness = %#v", readiness)
	}
	choices, ok := readiness.Prompt["choices"].([]map[string]string)
	if !ok || len(choices) != 2 || choices[1]["id"] != "manual_chart" {
		t.Fatalf("prompt = %#v", readiness.Prompt)
	}
}

func TestAccountingReadinessRecognizesBuiltInAssetsThenBecomesReady(t *testing.T) {
	connections := []orgreports.AccountingConnection{{ID: "ac-1", Type: "manual"}}
	readiness := buildAccountingReadiness(connections, nil, nil)
	if readiness.Decision != "client_categories_and_contacts_needed" || !readiness.InteractionRequired || !readiness.BuiltInDigitalAssets {
		t.Fatalf("readiness = %#v", readiness)
	}
	readiness = buildAccountingReadiness(connections, []orgreports.Category{{ID: "cat-1", Enabled: true, AccountingConnectionID: "ac-1"}}, []orgreports.Contact{{ID: "contact-1", Enabled: true, AccountingConnectionID: "ac-1"}})
	if !readiness.ReadyForRules || readiness.InteractionRequired || readiness.Decision != "ready_for_categorization_and_rules" {
		t.Fatalf("readiness = %#v", readiness)
	}
}

func TestChartAccountValidationAndImportShape(t *testing.T) {
	account := chartAccountInput{ConnectionID: "ac-1", ID: "4000", Name: "Revenue", Type: "Revenue", Code: "4000"}
	if err := validateChartAccount(account); err != nil {
		t.Fatal(err)
	}
	request := accountRequest(account)
	if request.Source != "manual" || request.Type != "revenue" || request.ID != "4000" {
		t.Fatalf("request = %#v", request)
	}
	loaded, err := loadChartAccounts("-", strings.NewReader(`{"accounts":[{"connectionId":"ac-1","id":"5000","name":"Fees","type":"expense"}]}`))
	if err != nil || len(loaded) != 1 || loaded[0].ID != "5000" {
		t.Fatalf("loaded = %#v err=%v", loaded, err)
	}
	account.Type = "made-up"
	if err := validateChartAccount(account); err == nil {
		t.Fatal("expected invalid account type")
	}
}

func TestChartAccountValidationRejectsAutomaticDigitalAssets(t *testing.T) {
	err := validateChartAccount(chartAccountInput{
		ConnectionID: "ac-1",
		ID:           "1000",
		Name:         "Digital Assets",
		Type:         "asset",
	})
	if err == nil || !strings.Contains(err.Error(), "provided automatically") {
		t.Fatalf("expected automatic Digital Assets error, got %v", err)
	}
}

func TestOrgAccountingHelpExplainsExternalAndManualChoice(t *testing.T) {
	cmd := newOrgAccountingCmd()
	if !strings.Contains(cmd.Long, "external accounting system") || !strings.Contains(cmd.Long, "manual") {
		t.Fatalf("help = %q", cmd.Long)
	}
}
