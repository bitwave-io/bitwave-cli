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
		case "/v3/orgs/org-1/transactions/bulk-state":
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
		case "/orgs/org-1/transactions/txn-review":
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"state":{"state":"open-needs-review","txn":{"transactionId":"txn-review"},"needsReview":{"pendingTxnLines":[]}}}`))
		case "/v3/orgs/org-1/transactions/resolve":
			if r.Method != http.MethodPut {
				t.Fatalf("request = %s %s", r.Method, r.URL.String())
			}
			var body TransactionReviewRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Transactions) != 1 || body.Transactions[0].TransactionID != "txn-review" {
				t.Fatalf("body = %#v", body)
			}
			if r.URL.Query().Get("action") == TransactionReviewAccept {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if r.URL.Query().Get("action") != TransactionReviewIgnore {
				t.Fatalf("action = %q", r.URL.Query().Get("action"))
			}
			_, _ = w.Write([]byte(`{"success":true,"processed":1,"successCount":1,"failed":[]}`))
		case "/v3/orgs/org-1/transactions":
			if r.Method != http.MethodPut {
				t.Fatalf("method = %s", r.Method)
			}
			_, _ = w.Write([]byte(`[{"success":true,"txnId":"txn-1"}]`))
		case "/v3/orgs/org-1/transactions/search":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			var body TransactionSearchRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Limit != 25 || len(body.Filters.FromAddresses) != 1 {
				t.Fatalf("body = %#v", body)
			}
			_, _ = w.Write([]byte(`{"transactions":[{"id":"txn-2"}],"nextToken":"next-1"}`))
		case "/v3/orgs/org-1/transactions/count":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			var body TransactionOverviewRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Filters.States) != 1 || body.Filters.States[0] != "failed-to-price" {
				t.Fatalf("body = %#v", body)
			}
			_, _ = w.Write([]byte(`{"count":7}`))
		case "/orgs/org-1/transactions/summary_v2":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"needsCategorization":10,"toBeReconciled":4,"needsReview":2,"all":20,"firstRecordDate":"2026-01-01T00:00:00Z"}`))
		case "/txns/orgs/org-1/transactions":
			if r.Method != http.MethodPost || r.URL.Query().Get("immediate") != "true" {
				t.Fatalf("request = %s %s", r.Method, r.URL.String())
			}
			var body []CreateTransaction
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 1 || body[0].TransactionType != "deposit" || body[0].AmountTicker != "USD" {
				t.Fatalf("body = %#v", body)
			}
			_, _ = w.Write([]byte(`{"transactions":[{"transactionId":"txn-created"}]}`))
		case "/graphql":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			var body struct {
				Variables struct {
					OrgID string                `json:"orgId"`
					Input InternalTransferInput `json:"input"`
				} `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Variables.OrgID != "org-1" || body.Variables.Input.FromWalletID != "wallet-a" {
				t.Fatalf("body = %#v", body)
			}
			_, _ = w.Write([]byte(`{"data":{"createInternalTransfer":{"id":"txn-transfer"}}}`))
		case "/org/org-1/categories":
			if r.Method == http.MethodPost {
				var body CreateChartAccountInput
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.ConnectionID != "ac-1" || body.Type != "revenue" || body.Source != "manual" {
					t.Fatalf("chart account body = %#v", body)
				}
				_, _ = w.Write([]byte(`{"id":"cat-created"}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"cat-1","name":"Revenue","enabled":true,"accountingConnectionId":"ac-1"}]}`))
		case "/contacts/org-1":
			_, _ = w.Write([]byte(`{"items":[{"id":"contact-1","name":"Customer","enabled":true,"accountingConnectionId":"ac-1"}]}`))
		case "/orgs/org-1/accounting-connections":
			_, _ = w.Write([]byte(`{"connections":[{"id":"ac-1","name":"Manual","type":"manual","disabled":false}]}`))
		case "/orgs/org-1/connections/manual":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"connectionId":"ac-created"}`))
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
	review, err := c.TransactionReviewDetail(ctx, "org-1", "txn-review")
	if err != nil || !json.Valid(review) {
		t.Fatalf("review = %s err=%v", review, err)
	}
	resolved, err := c.ResolveTransactionReviews(ctx, "org-1", TransactionReviewIgnore, []string{"txn-review"})
	if err != nil || !resolved.Success || resolved.SuccessCount != 1 {
		t.Fatalf("resolved = %#v err=%v", resolved, err)
	}
	accepted, err := c.ResolveTransactionReviews(ctx, "org-1", TransactionReviewAccept, []string{"txn-review"})
	if err != nil || !accepted.Success || accepted.SuccessCount != 1 {
		t.Fatalf("accepted = %#v err=%v", accepted, err)
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
	manual, err := c.CreateManualAccountingConnection(ctx, "org-1")
	if err != nil || manual.ConnectionID != "ac-created" {
		t.Fatalf("manual = %#v err=%v", manual, err)
	}
	account, err := c.CreateChartAccount(ctx, "org-1", CreateChartAccountInput{ConnectionID: "ac-1", Source: "manual", ID: "4000", Name: "Revenue", Type: "revenue", Code: "4000"})
	if err != nil || account.ID != "cat-created" {
		t.Fatalf("account = %#v err=%v", account, err)
	}
	search, err := c.SearchTransactions(ctx, "org-1", TransactionSearchRequest{Limit: 25, Filters: TransactionExportFilters{FromAddresses: []string{"0xabc"}}})
	if err != nil || len(search.Transactions) != 1 || search.NextToken != "next-1" {
		t.Fatalf("search = %#v err=%v", search, err)
	}
	count, err := c.TransactionCount(ctx, "org-1", TransactionOverviewRequest{Filters: TransactionExportFilters{States: []string{"failed-to-price"}}})
	if err != nil || count != 7 {
		t.Fatalf("count = %d err=%v", count, err)
	}
	overview, err := c.TransactionOverview(ctx, "org-1", TransactionOverviewRequest{})
	if err != nil || overview.All != 20 || overview.NeedsCategorization != 10 || overview.FirstRecordDate == nil {
		t.Fatalf("overview = %#v err=%v", overview, err)
	}
	created, err := c.CreateTransactions(ctx, "org-1", []CreateTransaction{{SystemID: "source-1", Time: "2026-08-10T10:00:00Z", AccountID: "wallet-a", Amount: "1.25", AmountTicker: "USD", TransactionType: "deposit"}})
	if err != nil || !json.Valid(created) {
		t.Fatalf("created = %s err=%v", created, err)
	}
	transfer, err := c.CreateInternalTransfer(ctx, "org-1", InternalTransferInput{FromWalletID: "wallet-a", ToWalletID: "wallet-b", Coin: "ETH", Amount: "1", CreatedSEC: 1})
	if err != nil || !json.Valid(transfer) {
		t.Fatalf("transfer = %s err=%v", transfer, err)
	}
}

func TestCreateTransactionLeavesCurrencyTypeImplicit(t *testing.T) {
	payload, err := json.Marshal(CreateTransaction{
		SystemID:        "source-usd",
		Time:            "2026-08-10T10:00:00Z",
		AccountID:       "wallet-a",
		Amount:          "1.25",
		AmountTicker:    "USD",
		TransactionType: "deposit",
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	if body["amountTicker"] != "USD" {
		t.Fatalf("amountTicker = %#v", body["amountTicker"])
	}
	if _, forcedType := body["type"]; forcedType {
		t.Fatalf("currency type must be omitted so the API can resolve USD as fiat: %s", payload)
	}
}

func TestAccountingResourceMutationContracts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Variables["orgId"] != "org-1" {
			t.Fatalf("variables = %#v", request.Variables)
		}
		switch request.OperationName {
		case "UpdateContact":
			contact, ok := request.Variables["contact"].(map[string]any)
			if !ok || contact["id"] != "contact-1" || contact["enabled"] != false {
				t.Fatalf("contact = %#v", request.Variables["contact"])
			}
			_, _ = w.Write([]byte(`{"data":{"updateContact":{"id":"contact-1","name":"Vendor","enabled":false}}}`))
		case "DisableContacts":
			_, _ = w.Write([]byte(`{"data":{"disableContacts":{"success":true}}}`))
		case "UpdateCategory":
			if request.Variables["id"] != "cat-1" || request.Variables["enabled"] != false {
				t.Fatalf("variables = %#v", request.Variables)
			}
			_, _ = w.Write([]byte(`{"data":{"updateCategory":{"id":"cat-1","name":"Revenue","enabled":false}}}`))
		case "DisableCategories":
			_, _ = w.Write([]byte(`{"data":{"disableCategories":{"success":true}}}`))
		default:
			t.Fatalf("operation = %q", request.OperationName)
		}
	}))
	defer srv.Close()

	client := New(srv.URL, func() (string, error) { return "token", nil })
	client.RulesMutationURL = srv.URL
	ctx := context.Background()
	contact, err := client.UpdateContact(ctx, "org-1", json.RawMessage(`{"id":"contact-1","enabled":false}`))
	if err != nil || !json.Valid(contact) {
		t.Fatalf("contact = %s err=%v", contact, err)
	}
	if err := client.DisableContacts(ctx, "org-1"); err != nil {
		t.Fatal(err)
	}
	category, err := client.UpdateCategoryEnabled(ctx, "org-1", "cat-1", false)
	if err != nil || !json.Valid(category) {
		t.Fatalf("category = %s err=%v", category, err)
	}
	if err := client.DisableCategories(ctx, "org-1"); err != nil {
		t.Fatal(err)
	}
}
