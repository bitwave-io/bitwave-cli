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

func TestProductionRuleEndpoints(t *testing.T) {
	client := New("https://api.bitwave.io", func() (string, error) { return "", nil })
	if client.RulesQueryURL != "https://api4.bitwave.io/graphql-reports" {
		t.Fatalf("query URL = %s", client.RulesQueryURL)
	}
	if client.RulesMutationURL != "https://api-app.bitwave.io/graphql" {
		t.Fatalf("mutation URL = %s", client.RulesMutationURL)
	}
}
