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

func TestDustInflowRequiresBoundedWalletAssetAndBuildsQuantityRule(t *testing.T) {
	payload, err := Build(Plan{
		Preset: "dust-inflow", Name: "Small ETH receipts", Priority: 1, Enabled: true,
		AccountingConnectionID: "Manual", CategoryID: "Manual.dust-income", ContactID: "Manual.dust-sender",
		FeeCategoryID: "Manual.gas", FeeContactID: "Manual.gas", WalletID: "wallet-1", Asset: "ETH", MaxAssetQty: "0.0001",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Transfer struct {
			ValueRules []struct{ Comparison, Value string } `json:"valueRules"`
			MultiToken bool                                 `json:"multiToken"`
			Coin       string                               `json:"coin"`
			WalletID   string                               `json:"walletId"`
		} `json:"transfer"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Transfer.ValueRules) != 1 || decoded.Transfer.ValueRules[0].Comparison != "LTE" || decoded.Transfer.ValueRules[0].Value != "0.0001" {
		t.Fatalf("value rules = %#v", decoded.Transfer.ValueRules)
	}
	if decoded.Transfer.MultiToken || decoded.Transfer.Coin != "ETH" || decoded.Transfer.WalletID != "wallet-1" {
		t.Fatalf("dust scope = %#v", decoded.Transfer)
	}
	if _, err := Build(Plan{Preset: "dust-inflow", Name: "unsafe", Priority: 1, AccountingConnectionID: "Manual", CategoryID: "cat", ContactID: "contact", Asset: "ETH", MaxAssetQty: "1"}); err == nil {
		t.Fatal("expected wallet requirement")
	}
}

func TestPlanningHierarchyStartsWithOrganizationWideTypes(t *testing.T) {
	hierarchy := PlanningHierarchy()
	if len(hierarchy) != 3 || hierarchy[0].Tier != 1 || hierarchy[1].Tier != 2 || hierarchy[2].Tier != 3 {
		t.Fatalf("hierarchy = %#v", hierarchy)
	}
	if !hierarchy[0].RecommendedStart || !hierarchy[0].ApplyAsSingleBatch {
		t.Fatalf("tier 1 must be the fast recommended starting batch: %#v", hierarchy[0])
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
	if len(hierarchy[2].Presets) != 1 || hierarchy[2].Presets[0] != "catch-all-clearing" || hierarchy[2].RecommendedStart {
		t.Fatalf("catch-all tier = %#v", hierarchy[2])
	}
}

func TestCatchAllClearingDefaultsToMultiTokenAndExposesAccountingRisk(t *testing.T) {
	recipe, ok := Find("catch-all-clearing")
	if !ok || recipe.PlanningTier != 3 || recipe.RecommendedPriority != 3 || !recipe.DefaultMulti || recipe.ConfirmationPrompt == "" || len(recipe.Prerequisites) != 2 {
		t.Fatalf("catch-all recipe = %#v", recipe)
	}
	payload, err := Build(Plan{
		Preset: "catch-all-clearing", Name: "Remaining to clearing", Priority: 3, Enabled: true,
		AccountingConnectionID: "Manual", CategoryID: "Manual.clearing", ContactID: "Manual.vendor",
		FeeCategoryID: "Manual.gas", FeeContactID: "Manual.gas",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Transfer struct {
			Priority   int    `json:"priority"`
			Direction  string `json:"direction"`
			MultiToken bool   `json:"multiToken"`
			Action     struct {
				Type       string `json:"type"`
				CategoryID string `json:"categoryId"`
			} `json:"action"`
		} `json:"transfer"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Transfer.Priority != 3 || decoded.Transfer.Direction != "All" || !decoded.Transfer.MultiToken || decoded.Transfer.Action.Type != "SimpleCategorization" || decoded.Transfer.Action.CategoryID != "Manual.clearing" {
		t.Fatalf("catch-all payload = %#v", decoded.Transfer)
	}
}

func TestGasFeeOnlyUsesDetailedFeeExtractor(t *testing.T) {
	payload, err := Build(Plan{
		Preset: "gas-fee-only", Name: "Gas Fee Only", Priority: 1, Enabled: true,
		AccountingConnectionID: "ac-1", FeeCategoryID: "ac-1.gas", FeeContactID: "ac-1.gas-contact",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Transfer struct {
			AccountingConnectionID string `json:"accountingConnectionId"`
			Direction              string `json:"direction"`
			AutoCategorizeFee      bool   `json:"autoCategorizeFee"`
			Action                 struct {
				Type  string `json:"type"`
				Lines []struct {
					ValueExtractor string `json:"valueExtractor"`
					AssetExtractor string `json:"assetExtractor"`
					CategoryID     string `json:"categoryId"`
					ContactID      string `json:"contactId"`
				} `json:"lines"`
			} `json:"action"`
		} `json:"transfer"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	transfer := decoded.Transfer
	if transfer.AccountingConnectionID != "ac-1" || transfer.Direction != "Outbound" || !transfer.AutoCategorizeFee {
		t.Fatalf("gas fee transfer = %#v", transfer)
	}
	if transfer.Action.Type == "InternalTransferCategorization" {
		t.Fatal("gas fee only must never serialize as an internal transfer")
	}
	if transfer.Action.Type != "DetailedCategorization" || len(transfer.Action.Lines) != 1 {
		t.Fatalf("gas fee action = %#v", transfer.Action)
	}
	line := transfer.Action.Lines[0]
	if line.ValueExtractor != "fee" || line.AssetExtractor != "COIN" || line.CategoryID != "ac-1.gas" || line.ContactID != "ac-1.gas-contact" {
		t.Fatalf("gas fee line = %#v", line)
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

func TestMetadataGuideUsesTransactionTypeToDisambiguateCantonMetadata(t *testing.T) {
	guide := MetadataGuide()
	found := map[string]string{}
	for _, pattern := range guide.GeneralPatterns {
		if pattern.Key == "FeeType" && pattern.Value == "holding_fees" {
			found[pattern.TransactionType] = pattern.SpecificCategory
		}
	}
	if found["AmuletRules_Transfer"] != "Idle Coin Transfer Fee" ||
		found["AmuletRules_BuyMemberTraffic"] != "Idle Coin Usage Fee" {
		t.Fatalf("holding fee patterns = %#v", found)
	}

	if len(guide.RuleArchetypes) == 0 || len(guide.AccountGuidance) == 0 || len(guide.DataQualityChecks) == 0 {
		t.Fatalf("Canton playbook is incomplete: %#v", guide)
	}
}
