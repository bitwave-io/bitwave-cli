package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
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
