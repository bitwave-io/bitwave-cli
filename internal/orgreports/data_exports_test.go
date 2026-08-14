package orgreports

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOrganizationDataExportContracts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v3/orgs/org-1":
			_, _ = w.Write([]byte(`{"id":"org-1","name":"Example","timezone":"America/New_York"}`))
		case "/orgs/org-1/inventory-views":
			_, _ = w.Write([]byte(`{"items":[{"id":"view-1","name":"Primary FIFO"}]}`))
		case "/v3/orgs/org-1/transactions/export":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			var body TransactionExportRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Timezone != "America/New_York" || body.Filters.DateRange == nil || body.Filters.DateRange.To != "2026-06-30" {
				t.Fatalf("transaction request = %#v", body)
			}
			_, _ = w.Write([]byte("Txn ID,Timestamp\ntxn-1,2026-06-30\n"))
		case "/orgs/org-1/inventory-views/view-1/actions":
			q := r.URL.Query()
			if q.Get("startDate") != "2026-01-01" || q.Get("asOf") != "2026-06-30" || q.Get("exportResults") != "true" {
				t.Fatalf("actions query = %v", q)
			}
			if got := q["action"]; len(got) != 2 || got[0] != "Buy" || got[1] != "Sell" {
				t.Fatalf("action filters = %v", got)
			}
			_, _ = w.Write([]byte(`{"exportIds":["export-1"]}`))
		case "/v2/orgs/org-1/exports/export-1":
			if r.URL.Query().Get("rawUrl") != "true" || r.URL.Query().Get("exportType") != "csv" {
				t.Fatalf("export query = %v", r.URL.Query())
			}
			_, _ = w.Write([]byte(`"https://storage.example/signed.csv"`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, func() (string, error) { return "token", nil })
	ctx := context.Background()
	org, err := c.Org(ctx, "org-1")
	if err != nil || org.Timezone != "America/New_York" {
		t.Fatalf("org = %#v err=%v", org, err)
	}
	views, err := c.InventoryViews(ctx, "org-1")
	if err != nil || len(views) != 1 || views[0].ID != "view-1" {
		t.Fatalf("views = %#v err=%v", views, err)
	}
	var csv bytes.Buffer
	err = c.StreamTransactionExport(ctx, "org-1", TransactionExportRequest{
		Timezone: "America/New_York",
		Filters: TransactionExportFilters{
			DateRange: &TransactionDateRange{From: "2026-01-01", To: "2026-06-30"},
		},
	}, &csv)
	if err != nil || csv.String() != "Txn ID,Timestamp\ntxn-1,2026-06-30\n" {
		t.Fatalf("csv = %q err=%v", csv.String(), err)
	}
	export, err := c.StartActionsExport(ctx, "org-1", "view-1", ActionsExportInput{
		From: "2026-01-01", To: "2026-06-30", Actions: []string{"Buy", "Sell"},
	})
	if err != nil || len(export.IDs()) != 1 || export.IDs()[0] != "export-1" {
		t.Fatalf("export = %#v err=%v", export, err)
	}
	href, err := c.ExportDownloadURL(ctx, "org-1", "export-1", "csv")
	if err != nil || href != "https://storage.example/signed.csv" {
		t.Fatalf("href = %q err=%v", href, err)
	}
}

func TestReportOptionDiscoveryContracts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs/org-1/wallets":
			if r.URL.Query().Get("paginationToken") == "next-1" {
				_, _ = w.Write([]byte(`{"items":[{"id":"wallet-2","name":"Trading"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"wallet-1","name":"Treasury","networkId":"ethereum"}],"nextPage":"next-1"}`))
		case "/orgs/org-1/subsidiaries":
			_, _ = w.Write([]byte(`[{"id":"sub-1","name":"Parent","subType":"root"}]`))
		case "/v3/orgs/org-1/transactions/search":
			var body TransactionAssetRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Filters.WalletIDs) != 1 || body.Filters.WalletIDs[0] != "wallet-1" {
				t.Fatalf("asset filters = %#v", body.Filters)
			}
			_, _ = w.Write([]byte(`{"transactions":[],"assetIds":["ETH","USDC"]}`))
		case "/orgs/org-1/inventory-views/view-1/actions/columns/asset/unique":
			if r.URL.Query().Get("pageToken") == "page-2" {
				_, _ = w.Write([]byte(`{"values":["USDC"]}`))
				return
			}
			_, _ = w.Write([]byte(`{"values":["ETH"],"nextPageToken":"page-2"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, func() (string, error) { return "token", nil })
	ctx := context.Background()
	wallets, err := c.Wallets(ctx, "org-1")
	if err != nil || len(wallets) != 2 {
		t.Fatalf("wallets = %#v err=%v", wallets, err)
	}
	subsidiaries, err := c.Subsidiaries(ctx, "org-1")
	if err != nil || len(subsidiaries) != 1 || subsidiaries[0].ID != "sub-1" {
		t.Fatalf("subsidiaries = %#v err=%v", subsidiaries, err)
	}
	assets, err := c.TransactionAssetIDs(ctx, "org-1", TransactionAssetRequest{Limit: 1, Filters: TransactionExportFilters{WalletIDs: []string{"wallet-1"}}})
	if err != nil || len(assets) != 2 {
		t.Fatalf("assets = %#v err=%v", assets, err)
	}
	actionAssets, err := c.ActionColumnValues(ctx, "org-1", "view-1", "asset", "2026-01-01", "2026-06-30")
	if err != nil || len(actionAssets) != 2 || actionAssets[1] != "USDC" {
		t.Fatalf("action assets = %#v err=%v", actionAssets, err)
	}
}
