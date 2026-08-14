package orgreports

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuleContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/graphql-reports":
			var request struct {
				OperationName string `json:"operationName"`
				Variables     struct {
					OrgID string `json:"orgId"`
				} `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.OperationName != "rules" || request.Variables.OrgID != "org-1" {
				t.Fatalf("request = %#v", request)
			}
			_, _ = w.Write([]byte(`{"data":{"rules":[{"id":"rule-1","name":"ETH inflows","disabled":true,"coin":"ETH"}]}}`))
		case "/graphql":
			var request struct {
				OperationName string `json:"operationName"`
				Variables     struct {
					OrgID string         `json:"orgId"`
					Rule  map[string]any `json:"rule"`
				} `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.OperationName != "CreateRule" || request.Variables.OrgID != "org-1" || request.Variables.Rule["transfer"] == nil {
				t.Fatalf("request = %#v", request)
			}
			_, _ = w.Write([]byte(`{"data":{"createRule":{"success":true,"errors":[]}}}`))
		case "/orgs/org-1/transactions/txn-1/rules/rule-1":
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"valid":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, func() (string, error) { return "token", nil })
	ctx := context.Background()
	rules, err := client.Rules(ctx, "org-1")
	if err != nil || len(rules) != 1 {
		t.Fatalf("rules = %#v err=%v", rules, err)
	}
	created, err := client.CreateRule(ctx, "org-1", json.RawMessage(`{"transfer":{"name":"ETH inflows"}}`))
	if err != nil || !created.Success {
		t.Fatalf("created = %#v err=%v", created, err)
	}
	validation, err := client.ValidateRule(ctx, "org-1", "txn-1", "rule-1")
	if err != nil || string(validation) != `{"valid":true}` {
		t.Fatalf("validation = %s err=%v", validation, err)
	}
}

func TestRuleReadAndMutationContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var request struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		switch request.OperationName {
		case "rule":
			_, _ = w.Write([]byte(`{"data":{"rule":{"id":"rule-1","name":"one","disabled":false,"priority":1,"direction":"Inbound","action":{"type":"Ignore","__typename":"IgnoreAction"},"__typename":"TransferRule"}}}`))
		case "rulesPaginated":
			_, _ = w.Write([]byte(`{"data":{"rulesPaginated":{"items":[{"id":"rule-1","name":"one","disabled":false,"priority":1,"direction":"Inbound","action":{"type":"Ignore","__typename":"IgnoreAction"},"__typename":"TransferRule"}],"nextPageToken":"next"}}}`))
		case "ToggleRuleStatus":
			if request.Variables["disabled"] != true {
				t.Fatalf("variables = %#v", request.Variables)
			}
			_, _ = w.Write([]byte(`{"data":{"toggleRuleStatus":true}}`))
		case "DeleteRule":
			_, _ = w.Write([]byte(`{"data":{"deleteRule":true}}`))
		default:
			t.Fatalf("operation = %q", request.OperationName)
		}
	}))
	defer server.Close()

	client := New(server.URL, func() (string, error) { return "token", nil })
	ctx := context.Background()
	rule, err := client.Rule(ctx, "org-1", "rule-1")
	if err != nil || !json.Valid(rule) {
		t.Fatalf("rule = %s err=%v", rule, err)
	}
	page, err := client.RulesPage(ctx, "org-1", 25, "")
	if err != nil || len(page.Items) != 1 || page.NextPageToken != "next" {
		t.Fatalf("page = %#v err=%v", page, err)
	}
	if err := client.ToggleRule(ctx, "org-1", "rule-1", true); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteRule(ctx, "org-1", "rule-1"); err != nil {
		t.Fatal(err)
	}
}

func TestProductionRuleEndpoints(t *testing.T) {
	client := New("https://api.bitwave.io", func() (string, error) { return "", nil })
	if client.RulesQueryURL != "https://api4.bitwave.io/graphql-reports" {
		t.Fatalf("query URL = %s", client.RulesQueryURL)
	}
	if client.RulesMutationURL != "https://api-app.bitwave.io/graphql" {
		t.Fatalf("mutation URL = %s", client.RulesMutationURL)
	}
}

func TestUpdateRunAndBulkRuleContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/org/org-1/rules/execute" {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["executeUpdates"] != "true" || body["after"] != float64(100) || body["before"] != float64(200) {
				t.Fatalf("bulk body = %#v", body)
			}
			_, _ = w.Write([]byte(`{"accepted":true}`))
			return
		}
		var request struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		switch request.OperationName {
		case "UpdateRule":
			if request.Variables["ruleId"] != "rule-1" {
				t.Fatalf("variables = %#v", request.Variables)
			}
			_, _ = w.Write([]byte(`{"data":{"updateRule":{"success":true,"errors":[]}}}`))
		case "RunRulesForOrg":
			_, _ = w.Write([]byte(`{"data":{"runRulesForOrg":true}}`))
		default:
			t.Fatalf("operation = %q", request.OperationName)
		}
	}))
	defer server.Close()

	client := New(server.URL, func() (string, error) { return "token", nil })
	client.RulesMutationURL = server.URL
	ctx := context.Background()
	if _, err := client.UpdateRule(ctx, "org-1", "rule-1", json.RawMessage(`{"transfer":{"name":"updated"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := client.RunRules(ctx, "org-1"); err != nil {
		t.Fatal(err)
	}
	if err := client.ExecuteBulkRules(ctx, "org-1", 100, 200); err != nil {
		t.Fatal(err)
	}
}
