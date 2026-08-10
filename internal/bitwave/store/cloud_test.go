package store

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bitwave-io/bitwave-accounting-sdk/model"
)

func TestCloudProjectUsesCurrentWorkspaceRoutes(t *testing.T) {
	t.Helper()
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("missing bearer token")
		}
		seen[r.URL.Path] = true
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/workspaces/ws-1":
			_, _ = w.Write([]byte(`{"id":"ws-1","name":"Cloud Books","baseCurrency":"USD"}`))
		case "/v1/workspaces/ws-1/ledger/accounts":
			_, _ = w.Write([]byte(`[
				{"id":"a-cash","name":"Assets:Cash","type":"ASSET"},
				{"id":"a-sales","name":"Income:Sales","type":"INCOME"}
			]`))
		case "/v1/workspaces/ws-1/ledger/commodities":
			_, _ = w.Write([]byte(`[{"symbol":"USD","default":true}]`))
		case "/v1/workspaces/ws-1/ledger/entries":
			_, _ = w.Write([]byte(`[{"id":"e-1","entryDate":"2026-06-30","payee":"Customer","status":"CLEARED","postings":[{"accountId":"a-cash","quantity":"100","asset":"USD"},{"accountId":"a-sales","quantity":"-100","asset":"USD"}]}]`))
		case "/v1/workspaces/ws-1/ledger/prices":
			_, _ = w.Write([]byte(`[{"priceDate":"2026-06-30","asset":"BTC","quoteCurrency":"USD","price":"60000"}]`))
		default:
			t.Fatalf("unexpected route: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cloud := NewCloud(server.URL, "org-1", "ws-1", func() (string, error) { return "test-token", nil })
	project, err := cloud.Project(context.Background())
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if project.Name != "Cloud Books" || project.BaseCurrency != "USD" {
		t.Fatalf("unexpected project metadata: %#v", project)
	}
	if len(project.Accounts) != 2 || len(project.Entries) != 1 || len(project.Prices) != 1 || len(project.Commodities) != 1 {
		t.Fatalf("unexpected project contents: %#v", project)
	}
	if got := project.Entries[0].Postings[0].Account; got != "Assets:Cash" {
		t.Fatalf("posting account = %q, want Assets:Cash", got)
	}
	for _, path := range []string{
		"/v1/workspaces/ws-1",
		"/v1/workspaces/ws-1/ledger/accounts",
		"/v1/workspaces/ws-1/ledger/commodities",
		"/v1/workspaces/ws-1/ledger/entries",
		"/v1/workspaces/ws-1/ledger/prices",
	} {
		if !seen[path] {
			t.Errorf("route %s was not called", path)
		}
	}
	for path := range seen {
		if strings.HasPrefix(path, "/api/v1/orgs/") {
			t.Errorf("legacy route was called: %s", path)
		}
	}
}

func TestCloudAddAccountUsesCurrentWorkspaceRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/workspaces/ws-1/ledger/accounts" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body accountDTO
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Name != "Assets:Cash" || body.Type != "ASSET" {
			t.Fatalf("unexpected body: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"a-1","name":"Assets:Cash","type":"ASSET"}`))
	}))
	defer server.Close()

	cloud := NewCloud(server.URL, "org-1", "ws-1", func() (string, error) { return "test-token", nil })
	if err := cloud.AddAccount(context.Background(), model.Account{Name: "Assets:Cash", Type: model.AccountAsset}); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
}

func TestCloudErrorDoesNotDumpHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<!doctype html><html><body>Cannot GET route</body></html>"))
	}))
	defer server.Close()

	cloud := NewCloud(server.URL, "org-1", "ws-1", func() (string, error) { return "test-token", nil })
	_, err := cloud.Project(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(strings.ToLower(err.Error()), "doctype") || strings.Contains(err.Error(), "<html>") {
		t.Fatalf("HTML leaked into error: %v", err)
	}
}
