package cmd

import (
	"bytes"
	"encoding/json"
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
		"--org", "org-1", "--preset", "simple-inflow", "--name", "ETH revenue",
		"--accounting-connection-id", "ac-1", "--category-id", "ac-1.cat",
		"--contact-id", "ac-1.contact", "--asset", "ETH", "--enabled", "--dry-run",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Status string `json:"status"`
		Plans  []struct {
			Payload json.RawMessage `json:"payload"`
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
			MultiToken bool `json:"multiToken"`
			Disabled   bool `json:"disabled"`
		} `json:"transfer"`
	}
	if err := json.Unmarshal(result.Plans[0].Payload, &payload); err != nil || payload.Transfer.MultiToken || payload.Transfer.Disabled {
		t.Fatalf("payload = %#v err=%v", payload, err)
	}
}

func TestReadAgentRuleSpecsAcceptsBatch(t *testing.T) {
	specs, err := readAgentRuleSpecs("-", agentRuleSpec{}, strings.NewReader(`[
      {"preset":"trade","name":"trades"},
      {"preset":"ignore-blank","name":"blank"}
    ]`))
	if err != nil || len(specs) != 2 || specs[0].Preset != "trade" {
		t.Fatalf("specs = %#v err=%v", specs, err)
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
		"--contact-id", "ac-1.vendor", "--metadata", "FeeType=receiver_lock_holding_fee",
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
		Patterns []rulerecipes.MetadataPattern `json:"patterns"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Patterns) != 3 {
		t.Fatalf("patterns = %#v", result.Patterns)
	}
}
