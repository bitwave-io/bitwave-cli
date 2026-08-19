package orgreports

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlatformDiscoveryEndpoints(t *testing.T) {
	var tokenHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenHeader = r.Header.Get("Authorization")
		switch {
		case r.URL.Path == "/v3/orgs/org-1/principals/aggregated":
			if r.URL.Query().Get("pageSize") != "25" {
				t.Fatalf("pageSize = %q", r.URL.Query().Get("pageSize"))
			}
			_, _ = w.Write([]byte(`{"items":[{"principalId":"p-1","principalEmail":"a@example.com","roles":[{"roleId":"r-1","roleName":"Admin"}]}]}`))
		case r.URL.Path == "/orgs/org-1/wallets":
			if r.URL.Query().Get("excludeDisabled") != "false" {
				t.Fatalf("excludeDisabled = %q", r.URL.Query().Get("excludeDisabled"))
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"w-1","name":"Old","disabled":true,"flags":{"syncStartDateSEC":123}}]}`))
		case r.URL.Path == "/orgs/org-1/lookups":
			_, _ = w.Write([]byte(`{"values":["ETH","ETH","USDC"]}`))
		case r.URL.Path == "/orgs/org-1/transactions/summary_v2":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["filters"] == nil {
				t.Fatalf("missing filters: %#v", body)
			}
			_, _ = w.Write([]byte(`{"all":9,"needsCategorization":2,"toBeReconciled":3,"firstRecordDate":"2024-01-01"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, func() (string, error) { return "test-token", nil })
	ctx := context.Background()
	users, err := client.OrgPrincipals(ctx, "org-1", 25)
	if err != nil || len(users.Items) != 1 || users.Items[0].Roles[0].RoleName != "Admin" {
		t.Fatalf("users = %#v, err = %v", users, err)
	}
	wallets, err := client.DetailedOrgWallets(ctx, "org-1")
	if err != nil || len(wallets) != 1 || !wallets[0].Disabled || wallets[0].Flags.SyncStartDateSEC != 123 {
		t.Fatalf("wallets = %#v, err = %v", wallets, err)
	}
	tokens, err := client.OrganizationTokens(ctx, "org-1")
	if err != nil || strings.Join(tokens, ",") != "ETH,USDC" {
		t.Fatalf("tokens = %#v, err = %v", tokens, err)
	}
	filters := TransactionCountFilters{IgnoredStatuses: []string{"Unignored"}}
	filters.DateRange.From, filters.DateRange.To = "2024-01-01", "2024-12-31"
	count, err := client.TransactionCount(ctx, "org-1", filters)
	if err != nil || count.All != 9 || count.NeedsCategorization != 2 {
		t.Fatalf("count = %#v, err = %v", count, err)
	}
	if tokenHeader != "Bearer test-token" {
		t.Fatalf("Authorization = %q", tokenHeader)
	}
}

func TestSetOrgWalletDisabledUsesGraphQLMutation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var request struct {
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Variables["walletId"] != "wallet-1" || request.Variables["disabled"] != true {
			t.Fatalf("variables = %#v", request.Variables)
		}
		_, _ = w.Write([]byte(`{"data":{"updateWallet":{"id":"wallet-1"}}}`))
	}))
	defer server.Close()
	client := New(server.URL, func() (string, error) { return "token", nil })
	if err := client.SetOrgWalletDisabled(context.Background(), "org-1", "wallet-1", "Treasury", true); err != nil {
		t.Fatal(err)
	}
}

func TestPublicLookupDoesNotSendOrganizationToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("public lookup leaked Authorization header %q", got)
		}
		_, _ = w.Write([]byte(`{"symbol":"ETH"}`))
	}))
	defer server.Close()
	client := New(server.URL, func() (string, error) { return "secret", nil })
	result, err := client.PublicSymbol(context.Background(), "ETH")
	if err != nil || !strings.Contains(string(result), "ETH") {
		t.Fatalf("result = %s, err = %v", result, err)
	}
}
