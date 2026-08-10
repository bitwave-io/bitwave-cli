package orgreports

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransactionMutationContracts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v3/orgs/org-1/transactions/bulk/state":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			var body BulkStateRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Update != TransactionStateIgnore || len(body.TransactionIDs) != 2 {
				t.Fatalf("body = %#v", body)
			}
			_, _ = w.Write([]byte(`{"success":true,"processed":2,"successCount":2,"failed":[]}`))
		case "/v3/orgs/org-1/transactions/txn-1":
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"id":"txn-1","state":"priced"}`))
				return
			}
			if r.Method != http.MethodPatch {
				t.Fatalf("method = %s", r.Method)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["type"] != "trade" {
				t.Fatalf("body = %#v", body)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v3/orgs/org-1/transactions":
			if r.Method != http.MethodPut {
				t.Fatalf("method = %s", r.Method)
			}
			_, _ = w.Write([]byte(`[{"success":true,"txnId":"txn-1"}]`))
		case "/org/org-1/categories":
			_, _ = w.Write([]byte(`{"items":[{"id":"cat-1","name":"Revenue","enabled":true,"accountingConnectionId":"ac-1"}]}`))
		case "/contacts/org-1":
			_, _ = w.Write([]byte(`{"items":[{"id":"contact-1","name":"Customer","enabled":true,"accountingConnectionId":"ac-1"}]}`))
		case "/orgs/org-1/accounting-connections":
			_, _ = w.Write([]byte(`{"connections":[{"id":"ac-1","name":"Manual","type":"manual","disabled":false}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, func() (string, error) { return "token", nil })
	ctx := context.Background()
	state, err := c.BulkUpdateTransactionState(ctx, "org-1", BulkStateRequest{TransactionIDs: []string{"txn-1", "txn-2"}, Update: TransactionStateIgnore})
	if err != nil || !state.Success || state.SuccessCount != 2 {
		t.Fatalf("state = %#v err=%v", state, err)
	}
	transaction, err := c.Transaction(ctx, "org-1", "txn-1")
	if err != nil || string(transaction) != `{"id":"txn-1","state":"priced"}` {
		t.Fatalf("transaction = %s err=%v", transaction, err)
	}
	if err := c.CategorizeTransaction(ctx, "org-1", "txn-1", json.RawMessage(`{"type":"trade"}`)); err != nil {
		t.Fatal(err)
	}
	bulk, err := c.BulkCategorizeTransactions(ctx, "org-1", json.RawMessage(`{"categorization":{"accountingConnectionId":"ac-1","trade":{}}}`))
	if err != nil || len(bulk) != 1 || !bulk[0].Success {
		t.Fatalf("bulk = %#v err=%v", bulk, err)
	}
	categories, err := c.Categories(ctx, "org-1")
	if err != nil || len(categories) != 1 || categories[0].ID != "cat-1" {
		t.Fatalf("categories = %#v err=%v", categories, err)
	}
	contacts, err := c.Contacts(ctx, "org-1")
	if err != nil || len(contacts) != 1 || contacts[0].ID != "contact-1" {
		t.Fatalf("contacts = %#v err=%v", contacts, err)
	}
	connections, err := c.AccountingConnections(ctx, "org-1")
	if err != nil || len(connections) != 1 || connections[0].ID != "ac-1" {
		t.Fatalf("connections = %#v err=%v", connections, err)
	}
}
