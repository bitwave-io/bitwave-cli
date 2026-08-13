package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRuleCreateDryRun(t *testing.T) {
	command := newCreateRawRuleCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetIn(strings.NewReader(`{"transfer":{"name":"ETH inflows","disabled":true,"priority":1,"accountingConnectionId":"ac-1","action":{"type":"SimpleCategorization"}}}`))
	command.SetArgs([]string{"--org", "org-1", "--input", "-", "--dry-run", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var envelope mutationEnvelope
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("output = %s err=%v", output.String(), err)
	}
	if envelope.Status != "preview" || envelope.Operation != "create-rule" || !envelope.DryRun {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestReadRuleObjectRequiresOneVariant(t *testing.T) {
	if _, err := readRuleObject("-", strings.NewReader(`{"transfer":{},"trade":{}}`)); err == nil {
		t.Fatal("expected multiple variant error")
	}
	rule, err := readRuleObject("-", strings.NewReader(`{"transfer":{"name":"test"}}`))
	if err != nil || !json.Valid(rule) {
		t.Fatalf("rule = %s err=%v", rule, err)
	}
}

func TestPrepareRuleObjectForcesDisabled(t *testing.T) {
	rule, err := prepareRuleObject(json.RawMessage(`{"transfer":{"name":"test","priority":1,"accountingConnectionId":"ac-1","action":{"type":"Ignore"}}}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var prepared struct {
		Transfer struct {
			Disabled bool `json:"disabled"`
		} `json:"transfer"`
	}
	if err := json.Unmarshal(rule, &prepared); err != nil || !prepared.Transfer.Disabled {
		t.Fatalf("rule = %s err=%v", rule, err)
	}
	if _, err := prepareRuleObject(json.RawMessage(`{"transfer":{"name":"test","priority":1,"accountingConnectionId":"ac-1","action":{"type":"Ignore"},"disabled":false}}`), false); err == nil {
		t.Fatal("expected enabled rule to require explicit permission")
	}
}

func TestSplitRuleDateWindows(t *testing.T) {
	windows, err := splitRuleDateWindows("2026-01-15", "2026-04-02", "UTC", 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []ruleDateWindow{{From: "2026-01-15", To: "2026-02-14"}, {From: "2026-02-15", To: "2026-03-14"}, {From: "2026-03-15", To: "2026-04-02"}}
	if len(windows) != len(want) {
		t.Fatalf("windows = %#v", windows)
	}
	for i := range want {
		if windows[i] != want[i] {
			t.Fatalf("window %d = %#v, want %#v", i, windows[i], want[i])
		}
	}
}
