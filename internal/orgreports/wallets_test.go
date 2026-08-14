package orgreports

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateOrgWallet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q", got)
		}
		var request struct {
			OperationName string `json:"operationName"`
			Variables     struct {
				OrgID  string             `json:"orgId"`
				Wallet map[string]any     `json:"wallet"`
				Prems  []WalletPermission `json:"prems"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.OperationName != "CreateOrgWallet" || request.Variables.OrgID != "org-1" {
			t.Fatalf("unexpected request: %#v", request)
		}
		if request.Variables.Wallet["type"] != "accountBasedBlockchain" {
			t.Fatalf("unexpected wallet: %#v", request.Variables.Wallet)
		}
		if request.Variables.Prems == nil {
			t.Fatal("prems must encode as a non-null array")
		}
		_, _ = w.Write([]byte(`{"data":{"createWallet":{"id":"wallet-1","name":"Treasury","networkId":"sol","address":"abc"}}}`))
	}))
	defer server.Close()

	client := New(server.URL, func() (string, error) { return "token", nil })
	client.RulesMutationURL = server.URL
	wallet, err := client.CreateOrgWallet(context.Background(), "org-1", map[string]any{
		"type": "accountBasedBlockchain",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if wallet.ID != "wallet-1" || wallet.NetworkID != "sol" {
		t.Fatalf("unexpected wallet: %#v", wallet)
	}
}

func TestOrgWalletsReturnsGraphQLError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"not allowed"}]}`))
	}))
	defer server.Close()
	client := New(server.URL, func() (string, error) { return "token", nil })
	client.RulesMutationURL = server.URL
	if _, err := client.OrgWallets(context.Background(), "org-1"); err == nil {
		t.Fatal("expected GraphQL error")
	}
}
