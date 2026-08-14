package orgreports

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWalletRollupContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/org-1/wallets/wallet-1/rollup" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"address":"0xabc","type":"babel","rules":[]}`))
		case http.MethodPost:
			var input WalletRollupRequest
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.Address != "0xabc" || len(input.Rules) != 1 {
				t.Fatalf("input = %#v", input)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("method = %s", r.Method)
		}
	}))
	defer server.Close()

	client := New(server.URL, func() (string, error) { return "token", nil })
	if _, err := client.WalletRollup(context.Background(), "org-1", "wallet-1"); err != nil {
		t.Fatal(err)
	}
	input := WalletRollupRequest{Address: "0xabc", Type: "babel", Rules: []BabelRollupRule{{RuleName: "daily"}}}
	if err := client.UpsertWalletRollup(context.Background(), "org-1", "wallet-1", input); err != nil {
		t.Fatal(err)
	}
}
