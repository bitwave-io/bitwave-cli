package orgreports

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransactionSummaryAddresses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dashboard/org-1/txns_summary/interacting_address/records" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("pagination[pageNumber]") != "1" || r.URL.Query().Get("pagination[pageSize]") != "100" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"items":[{"walletId":"wallet-1","interactingAddress":"0x1234567890abcdef","depositsTxnsCount":140,"depositsUncategorized":120,"withdrawalsTxnsCount":4,"withdrawalsUncategorized":3}]}`))
	}))
	defer server.Close()
	client := New(server.URL, func() (string, error) { return "token", nil })
	items, err := client.TransactionSummaryAddresses(context.Background(), "org-1", 1, 100)
	if err != nil || len(items) != 1 || items[0].DepositsUncategorized != 120 {
		t.Fatalf("items = %#v err=%v", items, err)
	}
}
