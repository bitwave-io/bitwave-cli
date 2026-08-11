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
	recipe, ok := Find("trade")
	if !ok || recipe.PlanningTier != 1 || recipe.DefaultScope != "organization" || recipe.Defaults["ignoreFailPricing"] != false {
		t.Fatalf("trade recipe hierarchy = %#v", recipe)
	}
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
	if transfer["autoCategorizeFee"] != false {
		t.Fatalf("trade fee must remain in trade treatment: %#v", transfer)
	}
	action, ok := transfer["action"].(map[string]any)
	if !ok || action["feeContactId"] != "Manual.1" {
		t.Fatalf("trade fee contact = %#v", action)
	}
	if _, exists := action["feeCategoryId"]; exists {
		t.Fatalf("trade must not include a fee category: %#v", action)
	}
}

func TestPlanningHierarchyStartsWithOrganizationWideTypes(t *testing.T) {
	hierarchy := PlanningHierarchy()
	if len(hierarchy) != 2 || hierarchy[0].Tier != 1 || hierarchy[1].Tier != 2 {
		t.Fatalf("hierarchy = %#v", hierarchy)
	}
	want := []string{"trade", "internal-transfer", "gas-fee-only"}
	for i, preset := range want {
		if hierarchy[0].Presets[i] != preset {
			t.Fatalf("tier 1 presets = %#v", hierarchy[0].Presets)
		}
		recipe, ok := Find(preset)
		if !ok || recipe.DefaultScope != "organization" {
			t.Fatalf("recipe %q = %#v", preset, recipe)
		}
	}
}

func TestDetailedRecipeRequiresRawContract(t *testing.T) {
	if _, err := Build(Plan{Preset: "detailed-categorization", Name: "details", Priority: 1, AccountingConnectionID: "ac-1"}); err == nil {
		t.Fatal("expected guidance-only preset to reject compact apply")
	}
}

func TestMetadataCategorizationBuildsMetadataCondition(t *testing.T) {
	payload, err := Build(Plan{
		Preset: "metadata-categorization", Name: "Canton receiver fee", Priority: 1,
		AccountingConnectionID: "ac-1", CategoryID: "ac-1.expense", ContactID: "ac-1.canton",
		MethodID: "0xe8e33700", MetadataOperator: "or", MetadataTransactionRecord: true,
		Metadata: []MetadataPair{{Key: "FeeType", Value: "receiver_lock_holding_fee"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Transfer struct {
			MethodID     string `json:"methodId"`
			MetadataRule struct {
				Operator      string         `json:"operator"`
				Metadata      []MetadataPair `json:"metadata"`
				TxnRecordRule bool           `json:"txnRecordRule"`
			} `json:"metadataRule"`
		} `json:"transfer"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	condition := decoded.Transfer.MetadataRule
	if condition.Operator != "OR" || !condition.TxnRecordRule || len(condition.Metadata) != 1 || condition.Metadata[0].Key != "FeeType" || decoded.Transfer.MethodID != "0xe8e33700" {
		t.Fatalf("metadata condition = %#v", condition)
	}
}

func TestMetadataGuideIncludesDocumentedCantonPattern(t *testing.T) {
	guide := MetadataGuide()
	found := false
	for _, pattern := range guide.GeneralPatterns {
		if pattern.Key == "RewardType" && pattern.Value == "input_app_reward_amount" && pattern.SpecificCategory == "Application Interaction Rewards" {
			found = true
		}
	}
	if !found || guide.InternalTransferStatus == "" {
		t.Fatalf("metadata guide missing documented pattern or construction warning: %#v", guide)
	}
}
