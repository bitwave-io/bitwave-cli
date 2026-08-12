package cmd

import (
	"strings"
	"testing"

	"github.com/bitwave-io/bitwave-cli/internal/orgreports"
)

func TestAccountingReadinessPromptsForSetup(t *testing.T) {
	readiness := buildAccountingReadiness(nil, nil, nil)
	if readiness.ReadyForRules || !readiness.InteractionRequired || readiness.Decision != "client_categories_and_contacts_needed" || !readiness.DigitalAssetsAccountPresent {
		t.Fatalf("readiness = %#v", readiness)
	}
	if readiness.ConnectionCount != 1 || readiness.Connections[0].ID != implicitManualConnectionID {
		t.Fatalf("prompt = %#v", readiness.Prompt)
	}
}

func TestAccountingReadinessIncludesImplicitDigitalAssetsThenBecomesReady(t *testing.T) {
	connections := []orgreports.AccountingConnection{{ID: "ac-1", Type: "manual"}}
	readiness := buildAccountingReadiness(connections, nil, nil)
	if readiness.Decision != "client_categories_and_contacts_needed" || !readiness.InteractionRequired || !readiness.DigitalAssetsAccountPresent {
		t.Fatalf("readiness = %#v", readiness)
	}
	readiness = buildAccountingReadiness(connections, []orgreports.Category{{ID: "cat-1", Name: "Digital Assets", Enabled: true, AccountingConnectionID: "ac-1"}}, []orgreports.Contact{{ID: "contact-1", Enabled: true, AccountingConnectionID: "ac-1"}})
	if !readiness.ReadyForRules || readiness.InteractionRequired || readiness.Decision != "ready_for_categorization_and_rules" {
		t.Fatalf("readiness = %#v", readiness)
	}
	if !readiness.DigitalAssetsAccountPresent {
		t.Fatalf("readiness should report the inspected Digital Assets category: %#v", readiness)
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

func TestChartAccountValidationWarnsButAllowsPossibleDuplicateDigitalAssets(t *testing.T) {
	account := chartAccountInput{
		ConnectionID: "ac-1",
		ID:           "1000",
		Name:         "Digital Assets",
		Type:         "asset",
	}
	if err := validateChartAccount(account); err != nil {
		t.Fatalf("accounting guidance must not block execution: %v", err)
	}
	warnings := chartAccountAdvisories(account)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "still submit") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestStarterPolicyIsMinimalAndEncodesBitwaveGuardrails(t *testing.T) {
	policy := starterPolicy("ac-1")
	if len(policy.AutomaticAccounts) != 0 {
		t.Fatalf("automatic accounts = %#v", policy.AutomaticAccounts)
	}
	if len(policy.Categories) != 3 || len(policy.Contacts) != 3 {
		t.Fatalf("starter = %#v", policy)
	}
	for _, category := range policy.Categories {
		if category.ConnectionID != "ac-1" || strings.EqualFold(category.Name, "Digital Assets") {
			t.Fatalf("category = %#v", category)
		}
	}
	if policy.Contacts[2].Name != "Gas Fees" || policy.Contacts[2].Type != "Vendor" {
		t.Fatalf("gas contact = %#v", policy.Contacts[2])
	}
	joined := strings.Join(policy.Guardrails, " ")
	if !strings.Contains(joined, "no fee category") || !strings.Contains(joined, "user") {
		t.Fatalf("guardrails = %#v", policy.Guardrails)
	}
}

func TestImplicitManualConnectionCarriesBuiltInAccounts(t *testing.T) {
	connections := withImplicitManualConnection([]orgreports.AccountingConnection{{ID: "generated", Type: "manual"}})
	if len(connections) != 2 || connections[1].ID != implicitManualConnectionID {
		t.Fatalf("connections = %#v", connections)
	}
	policy := starterPolicy(implicitManualConnectionID)
	if len(policy.AutomaticAccounts) != 2 || policy.AutomaticAccounts[0] != "Digital Assets" || policy.AutomaticAccounts[1] != "Crypto Fees" {
		t.Fatalf("automatic accounts = %#v", policy.AutomaticAccounts)
	}
}

func TestOrgAccountingHelpExplainsExistingManualAndExternalChoice(t *testing.T) {
	cmd := newOrgAccountingCmd()
	if !strings.Contains(cmd.Long, "existing external") || !strings.Contains(cmd.Long, "must not create a generated") {
		t.Fatalf("help = %q", cmd.Long)
	}
}
