package orgreports

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFullWalletResync(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/hawaii/orgs/org-1/wallets/wallet-1/fullResync" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]bool
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !body["execute"] {
			t.Fatal("expected execute=true")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"dryRun":false,"orgId":"org-1","walletId":"wallet-1","syncer":"Solana","executionId":"execution-1"}`))
	}))
	defer server.Close()

	client := New(server.URL, func() (string, error) { return "token", nil })
	response, err := client.FullWalletResync(context.Background(), "org-1", "wallet-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.DryRun || response.ExecutionID != "execution-1" {
		t.Fatalf("unexpected response: %#v", response)
	}
}
