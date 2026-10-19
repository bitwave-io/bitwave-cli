package orgreports

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestTransactionTickerValuesRequestsAllValuesFromTransactionsService(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/org-1/lookups" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("fieldName"); got != "amountCurrencyName" {
			t.Fatalf("fieldName = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "-1" {
			t.Fatalf("limit = %q, want the all-values sentinel", got)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"values":["ETH","USDC","SPAMCOIN"]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, func() (string, error) { return "token", nil })
	c.TransactionsURL = srv.URL
	got, err := c.TransactionTickerValues(context.Background(), "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ETH", "USDC", "SPAMCOIN"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("values = %v, want %v", got, want)
	}
}

func TestTransactionTickerValuesRejectsMalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := New(srv.URL, func() (string, error) { return "token", nil })
	c.TransactionsURL = srv.URL
	if _, err := c.TransactionTickerValues(context.Background(), "org-1"); err == nil || !strings.Contains(err.Error(), "decode transaction ticker lookup") {
		t.Fatalf("err = %v", err)
	}
}
