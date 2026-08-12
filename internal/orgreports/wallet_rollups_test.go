package orgreports

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpsertWalletRollupContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/orgs/org-1/wallets/wallet-1/rollup" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var request WalletRollupRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Type != "rollup-by-time" || request.Address != "0xabc" || len(request.Rules) != 1 {
			t.Fatalf("request = %#v", request)
		}
		if request.Rules[0].FingerPrint != "simpleTrade" || request.Rules[0].RoundPeriod != "end-of-period" {
			t.Fatalf("rule = %#v", request.Rules[0])
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := New(server.URL, func() (string, error) { return "token", nil })
	err := client.UpsertWalletRollup(context.Background(), "org-1", "wallet-1", "0xabc", []BabelRollupRule{{
		RuleName: "Trades", Classification: "trades", FingerPrint: "simpleTrade", RollupAction: "rollup", Cadence: "hour", RoundPeriod: "end-of-period",
	}})
	if err != nil {
		t.Fatal(err)
	}
}
