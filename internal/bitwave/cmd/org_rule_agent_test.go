package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bitwave-io/bitwave-cli/internal/rulerecipes"
)

func TestRuleApplyDryRunWithIDsNeedsNoDiscovery(t *testing.T) {
	command := newRuleApplyCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"--org", "org-1", "--preset", "simple-inflow", "--id", "rule-1", "--name", "ETH revenue",
		"--accounting-connection-id", "ac-1", "--category-id", "ac-1.cat",
		"--contact-id", "ac-1.contact", "--fee-category-id", "ac-1.gas",
		"--fee-contact-id", "ac-1.gas-vendor", "--asset", "ETH", "--enabled", "--dry-run",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Status string `json:"status"`
		Plans  []struct {
			Payload  json.RawMessage `json:"payload"`
			Warnings []string        `json:"warnings"`
			Scope    struct {
				ActualScope string `json:"actualScope"`
				Recommended bool   `json:"recommended"`
				Risk        string `json:"risk"`
			} `json:"scopeAssessment"`
		} `json:"plans"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("output = %s err=%v", output.String(), err)
	}
	if result.Status != "preview" || len(result.Plans) != 1 {
		t.Fatalf("result = %#v output=%s", result, output.String())
	}
	var payload struct {
		Transfer struct {
			MultiToken bool   `json:"multiToken"`
			Disabled   bool   `json:"disabled"`
			ID         string `json:"id"`
		} `json:"transfer"`
	}
	if err := json.Unmarshal(result.Plans[0].Payload, &payload); err != nil || payload.Transfer.MultiToken || payload.Transfer.Disabled || payload.Transfer.ID != "" {
		t.Fatalf("payload = %#v err=%v", payload, err)
	}
	if len(result.Plans[0].Warnings) != 1 || !strings.Contains(result.Plans[0].Warnings[0], "no walletId") {
		t.Fatalf("wallet scope warnings = %#v", result.Plans[0].Warnings)
	}
	if result.Plans[0].Scope.ActualScope != "organization" || result.Plans[0].Scope.Recommended || result.Plans[0].Scope.Risk != "broad-simple-flow" {
		t.Fatalf("scope assessment = %#v", result.Plans[0].Scope)
	}
}

func TestReadAgentRuleSpecsAcceptsBatch(t *testing.T) {
	specs, err := readAgentRuleSpecs("-", agentRuleSpec{}, strings.NewReader(`[
      {"preset":"trade","name":"trades"},
      {"preset":"ignore-blank","name":"blank"}
    ]`))
	if err != nil || len(specs) != 2 || specs[0].Preset != "trade" || specs[0].Priority != 1 || specs[1].Priority != 1 {
		t.Fatalf("specs = %#v err=%v", specs, err)
	}
}

func TestReadAgentRuleSpecsAcceptsInlineJSON(t *testing.T) {
	specs, err := readAgentRuleSpecs(`[{"preset":"simple-inflow","walletId":"wallet-1"}]`, agentRuleSpec{}, strings.NewReader(""))
	if err != nil || len(specs) != 1 || specs[0].WalletID != "wallet-1" {
		t.Fatalf("specs = %#v err=%v", specs, err)
	}
}

func TestReadAgentRuleSpecsDefaultsOnlyOmittedPriority(t *testing.T) {
	specs, err := readAgentRuleSpecs("-", agentRuleSpec{}, strings.NewReader(`[
      {"preset":"simple-inflow"},
      {"preset":"simple-outflow","priority":0},
      {"preset":"trade","priority":7}
    ]`))
	if err != nil {
		t.Fatal(err)
	}
	if got := []int{specs[0].Priority, specs[1].Priority, specs[2].Priority}; got[0] != 1 || got[1] != 0 || got[2] != 7 {
		t.Fatalf("priorities = %#v", got)
	}

	single, err := readAgentRuleSpecs("-", agentRuleSpec{}, strings.NewReader(`{"preset":"simple-inflow"}`))
	if err != nil || len(single) != 1 || single[0].Priority != 1 {
		t.Fatalf("single = %#v err=%v", single, err)
	}
}

func TestConnectionDerivedFromStableAccountingItemIDs(t *testing.T) {
	if got := connectionFromItemID("QVvQMAmu6j89GZvqag3V.113"); got != "QVvQMAmu6j89GZvqag3V" {
		t.Fatalf("connection = %q", got)
	}
}

func TestMetadataRuleApplyDryRun(t *testing.T) {
	command := newRuleApplyCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"--org", "org-1", "--preset", "metadata-categorization", "--name", "Canton fee",
		"--accounting-connection-id", "ac-1", "--category-id", "ac-1.expense",
		"--contact-id", "ac-1.vendor", "--fee-category-id", "ac-1.gas",
		"--fee-contact-id", "ac-1.gas-vendor", "--metadata", "FeeType=receiver_lock_holding_fee",
		"--metadata-operator", "AND", "--method-id", "0xe8e33700", "--dry-run",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Plans []struct {
			Payload struct {
				Transfer struct {
					MethodID     string `json:"methodId"`
					MetadataRule struct {
						Operator string `json:"operator"`
						Metadata []struct {
							Key   string `json:"key"`
							Value string `json:"value"`
						} `json:"metadata"`
					} `json:"metadataRule"`
				} `json:"transfer"`
			} `json:"payload"`
		} `json:"plans"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("output = %s err=%v", output.String(), err)
	}
	condition := result.Plans[0].Payload.Transfer.MetadataRule
	if condition.Operator != "AND" || len(condition.Metadata) != 1 || condition.Metadata[0].Value != "receiver_lock_holding_fee" || result.Plans[0].Payload.Transfer.MethodID != "0xe8e33700" {
		t.Fatalf("metadata condition = %#v", condition)
	}
}

func TestSimpleRuleRejectsImplicitPrimaryFeeAccounting(t *testing.T) {
	command := newRuleApplyCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"--org", "org-1", "--preset", "simple-outflow", "--name", "Vendor payment",
		"--accounting-connection-id", "ac-1", "--category-id", "ac-1.expense",
		"--contact-id", "ac-1.vendor", "--asset", "ETH", "--dry-run",
	})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "separate fee lines") {
		t.Fatalf("error = %v output=%s", err, output.String())
	}

	command = newRuleApplyCmd()
	output.Reset()
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"--org", "org-1", "--preset", "simple-outflow", "--name", "Vendor payment",
		"--accounting-connection-id", "ac-1", "--category-id", "ac-1.expense",
		"--contact-id", "ac-1.vendor", "--asset", "ETH", "--no-auto-categorize-fee", "--dry-run",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("explicit fee opt-out failed: %v output=%s", err, output.String())
	}
}

func TestRuleConditionCandidatesPreferRepeatedMetadataAndMethodID(t *testing.T) {
	items := []compactTransaction{
		{ID: "txn-1", MethodID: "0x12345678", Metadata: map[string]any{"protocol": "Aave", "txHash": "one"}},
		{ID: "txn-2", MethodID: "0x12345678", Metadata: map[string]any{"protocol": "Aave", "txHash": "two"}},
		{ID: "txn-3", MethodID: "0x99999999", Metadata: map[string]any{"protocol": "Other"}},
	}
	candidates := ruleConditionCandidates(items, 20)
	if len(candidates) < 4 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].Assessment != "preferred-reusable-condition" || candidates[0].MatchCount != 2 {
		t.Fatalf("first candidate = %#v", candidates[0])
	}
	foundVolatile := false
	for _, candidate := range candidates {
		if candidate.Key == "txHash" && candidate.Assessment == "avoid-transaction-specific" {
			foundVolatile = true
		}
	}
	if !foundVolatile {
		t.Fatalf("transaction-specific metadata was not flagged: %#v", candidates)
	}
}

func TestRuleConditionCandidatesPreserveExactMetadataAndExposeScope(t *testing.T) {
	items := []compactTransaction{
		{ID: "txn-1", TransactionType: "receive", Metadata: map[string]any{"Event-Type": "Reward_Fee"}, Lines: []compactTransactionLine{{WalletID: "wallet-a", NetworkID: "network-a", AmountCurrencyName: "TOKEN"}}},
		{ID: "txn-2", TransactionType: "receive", Metadata: map[string]any{"Event-Type": "Reward_Fee"}, Lines: []compactTransactionLine{{WalletID: "wallet-a", NetworkID: "network-a", AmountCurrencyName: "TOKEN"}}},
		{ID: "txn-3", TransactionType: "send", Metadata: map[string]any{"event-type": "reward_fee"}, Lines: []compactTransactionLine{{WalletID: "wallet-b", NetworkID: "network-b", AmountCurrencyName: "OTHER"}}},
	}
	candidates := ruleConditionCandidates(items, 20)
	var exact *ruleConditionCandidate
	for index := range candidates {
		if candidates[index].Key == "Event-Type" && candidates[index].Value == "Reward_Fee" {
			exact = &candidates[index]
			break
		}
	}
	if exact == nil {
		t.Fatalf("exact metadata candidate missing: %#v", candidates)
	}
	if exact.MatchCount != 2 || exact.DistinctValues != 1 || exact.KeyOccurrences != 2 {
		t.Fatalf("candidate counts = %#v", exact)
	}
	if strings.Join(exact.WalletIDs, ",") != "wallet-a" || strings.Join(exact.TransactionTypes, ",") != "receive" || strings.Join(exact.NetworkIDs, ",") != "network-a" || strings.Join(exact.Assets, ",") != "TOKEN" {
		t.Fatalf("candidate scope = %#v", exact)
	}
}

func TestRuleConditionCandidatesRejectHighCardinalityMetadata(t *testing.T) {
	items := make([]compactTransaction, 5)
	for index := range items {
		items[index] = compactTransaction{ID: fmt.Sprintf("txn-%d", index), Metadata: map[string]any{"eventReference": fmt.Sprintf("unique-%d", index)}}
	}
	candidates := ruleConditionCandidates(items, 20)
	if len(candidates) != 5 {
		t.Fatalf("candidates = %#v", candidates)
	}
	for _, candidate := range candidates {
		if candidate.Assessment != "avoid-high-cardinality" {
			t.Fatalf("candidate was not rejected: %#v", candidate)
		}
	}
}

func TestCompactTransactionsPreservesRuleEvidence(t *testing.T) {
	items := compactTransactions([]json.RawMessage{json.RawMessage(`{
		"id":"txn-1","methodId":"0xa9059cbb","metadata":{"protocol":"Example"},"lines":[]
	}`)})
	if len(items) != 1 || items[0].MethodID != "0xa9059cbb" || items[0].Metadata["protocol"] != "Example" {
		t.Fatalf("items = %#v", items)
	}
}

func TestParseMetadataFlagsRejectsIncompletePair(t *testing.T) {
	if _, err := parseMetadataFlags([]string{"FeeType"}); err == nil {
		t.Fatal("expected KEY=VALUE validation error")
	}
}

func TestMetadataGuideFiltersKey(t *testing.T) {
	command := newRuleMetadataGuideCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--key", "RewardType", "--chart", "specific"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Patterns            []rulerecipes.MetadataPattern    `json:"patterns"`
		NetworkTerminology  []rulerecipes.NetworkTerm        `json:"networkTerminology"`
		AccountingDecisions []rulerecipes.AccountingDecision `json:"accountingDecisions"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Patterns) != 4 {
		t.Fatalf("patterns = %#v", result.Patterns)
	}
	if len(result.NetworkTerminology) < 2 || result.NetworkTerminology[0].Term != "partyId" {
		t.Fatalf("network terminology = %#v", result.NetworkTerminology)
	}
	if len(result.AccountingDecisions) < 7 || result.AccountingDecisions[0].SuggestedAction != "InternalTransferCategorization" {
		t.Fatalf("accounting decisions = %#v", result.AccountingDecisions)
	}
}
