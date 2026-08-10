package rulerecipes

import (
	"encoding/json"
	"testing"
)

func TestSimpleInflowUsesSingleTokenAndPrimaryFeeDefaults(t *testing.T) {
	payload, err := Build(Plan{
		Preset: "simple-inflow", Name: "ETH revenue", Priority: 1, Enabled: true,
		AccountingConnectionID: "ac-1", CategoryID: "ac-1.cat", ContactID: "ac-1.contact",
		FeeCategoryID: "ac-1.cat", FeeContactID: "ac-1.contact", Asset: "ETH",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Transfer struct {
			Disabled   bool   `json:"disabled"`
			Direction  string `json:"direction"`
			MultiToken bool   `json:"multiToken"`
			Action     struct {
				Type          string `json:"type"`
				CategoryID    string `json:"categoryId"`
				FeeCategoryID string `json:"feeCategoryId"`
			} `json:"action"`
		} `json:"transfer"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Transfer.Disabled || decoded.Transfer.Direction != "Inbound" || decoded.Transfer.MultiToken {
		t.Fatalf("transfer = %#v", decoded.Transfer)
	}
	if decoded.Transfer.Action.Type != "SimpleCategorization" || decoded.Transfer.Action.CategoryID != "ac-1.cat" || decoded.Transfer.Action.FeeCategoryID != "ac-1.cat" {
		t.Fatalf("action = %#v", decoded.Transfer.Action)
	}
}

func TestTradeRecipeForcesDocumentedDefaults(t *testing.T) {
	payload, err := Build(Plan{
		Preset: "trade", Name: "All trades", Priority: 1,
		AccountingConnectionID: "Manual", FeeContactID: "Manual.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	transfer := decoded["transfer"]
	if transfer["multiToken"] != true || transfer["allowMismatch"] != true || transfer["direction"] != "All" {
		t.Fatalf("transfer = %#v", transfer)
	}
}

func TestDetailedRecipeRequiresRawContract(t *testing.T) {
	if _, err := Build(Plan{Preset: "detailed-categorization", Name: "details", Priority: 1, AccountingConnectionID: "ac-1"}); err == nil {
		t.Fatal("expected guidance-only preset to reject compact apply")
	}
}
