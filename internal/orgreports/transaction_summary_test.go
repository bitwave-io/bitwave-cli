package orgreports

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransactionSummaryEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dashboard/org-1/txns_summary/main/records":
			if r.URL.Query().Get("base_filters[0][value]") != "2026-07-01" {
				t.Fatalf("wallet query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"items":[{"wallet":"Admin","walletId":"wallet-1","totalTxnsCount":20}]}`))
		case "/dashboard/org-1/txns_summary/interacting_address/records":
			if r.URL.Query().Get("base_filters[0][value][0]") != "wallet-1" || r.URL.Query().Get("sort[field]") != "depositsUncategorized" {
				t.Fatalf("address query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"items":[{"walletId":"wallet-1","interactingAddress":"0x1234567890abcdef","depositsUncategorized":12}]}`))
		case "/dashboard/org-1/txns_summary/assets":
			_, _ = w.Write([]byte(`{"items":[{"assetId":"COIN.1031","assetName":"TUSD"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, func() (string, error) { return "token", nil })
	wallets, err := client.TransactionSummaryWallets(context.Background(), "org-1", "2026-07-01", "2026-07-31", 1, 100)
	if err != nil || len(wallets) != 1 || wallets[0].WalletID != "wallet-1" {
		t.Fatalf("wallets = %#v err=%v", wallets, err)
	}
	addresses, err := client.TransactionSummaryAddresses(context.Background(), "org-1", "wallet-1", "", "", "depositsUncategorized", 1, 100)
	if err != nil || len(addresses) != 1 || addresses[0].InteractingAddress != "0x1234567890abcdef" {
		t.Fatalf("addresses = %#v err=%v", addresses, err)
	}
	assets, err := client.TransactionSummaryAssets(context.Background(), "org-1")
	if err != nil || len(assets) != 1 || assets[0].AssetName != "TUSD" {
		t.Fatalf("assets = %#v err=%v", assets, err)
	}
}
