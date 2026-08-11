package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestTransactionSpamAnalyzeExcludesMixedTokenTransactions(t *testing.T) {
	var bulkIgnoredIDs []string
	addressServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/symbols/SPAM" {
			t.Fatalf("address path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"coinId":999,"networkId":"eth","address":"0x999","symbol":"SPAM","spamScore":0.9}`))
	}))
	defer addressServer.Close()

	coreServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v3/orgs/org-1":
			_, _ = w.Write([]byte(`{"id":"org-1","timezone":"UTC"}`))
		case "/dashboard/org-1/txns_summary/assets":
			_, _ = w.Write([]byte(`{"items":[{"assetId":"COIN.999","assetName":"SPAM"}]}`))
		case "/v3/orgs/org-1/transactions/search":
			var body struct {
				Limit   int `json:"limit"`
				Filters struct {
					AssetIDs               []string `json:"assetIds"`
					CategorizationStatuses []string `json:"categorizationStatuses"`
					IgnoredStatuses        []string `json:"ignoredStatuses"`
				} `json:"filters"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Filters.AssetIDs) == 0 {
				if len(body.Filters.CategorizationStatuses) != 1 || body.Filters.CategorizationStatuses[0] != "Uncategorized" {
					t.Fatalf("facet scope = %#v", body.Filters)
				}
				_, _ = w.Write([]byte(`{"assetIds":["COIN.999"]}`))
				return
			}
			if len(body.Filters.CategorizationStatuses) != 1 || body.Filters.CategorizationStatuses[0] != "Uncategorized" || len(body.Filters.IgnoredStatuses) != 1 {
				t.Fatalf("transaction scope = %#v", body.Filters)
			}
			_, _ = w.Write([]byte(`{"transactions":[
				{"id":"txn-single","categorizationStatus":"Uncategorized","ignored":false,"lines":[{"line":0,"amountCurrencyId":"COIN.999","amountCurrencyName":"SPAM"}]},
				{"id":"txn-same-token","categorizationStatus":"Uncategorized","ignored":false,"lines":[{"line":0,"amountCurrencyId":"COIN.999"},{"line":1,"amountCurrencyId":"COIN.999"}]},
				{"id":"txn-trade","categorizationStatus":"Uncategorized","ignored":false,"lines":[{"line":0,"amountCurrencyId":"COIN.999"},{"line":1,"amountCurrencyId":"COIN.10"}]}
			]}`))
		case "/v3/orgs/org-1/transactions/bulk/state":
			var body struct {
				TransactionIDs []string `json:"transactionIds"`
				Update         string   `json:"update"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Update != "ignore" {
				t.Fatalf("bulk update = %#v", body)
			}
			bulkIgnoredIDs = append([]string(nil), body.TransactionIDs...)
			_, _ = w.Write([]byte(`{"success":true,"processed":2,"successCount":2,"failed":[]}`))
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer coreServer.Close()

	t.Setenv("BITWAVE_BASE_URL_CORE", coreServer.URL)
	t.Setenv("BITWAVE_ADDRESS_SERVICE_URL", addressServer.URL)
	t.Setenv("BITWAVE_TOKEN", "token")
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runTransactionSpamAnalyze(cmd, "org-1", 4, 100, 100, 0.5, false, nil); err != nil {
		t.Fatal(err)
	}
	var result struct {
		TransactionScope string   `json:"transactionScope"`
		IgnoreReadyCount int      `json:"ignoreReadyCount"`
		IgnoreIDs        []string `json:"ignoreTransactionIds"`
		Plans            []struct {
			ExcludedMixedTokenCount int `json:"excludedMixedTokenCount"`
		} `json:"spamAssetPlans"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output = %s err=%v", out.String(), err)
	}
	if result.TransactionScope != "uncategorized-only" || result.IgnoreReadyCount != 2 || len(result.IgnoreIDs) != 2 || result.IgnoreIDs[0] != "txn-single" || result.IgnoreIDs[1] != "txn-same-token" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Plans) != 1 || result.Plans[0].ExcludedMixedTokenCount != 1 {
		t.Fatalf("plans = %#v", result.Plans)
	}

	cmd = &cobra.Command{}
	cmd.SetContext(context.Background())
	out.Reset()
	cmd.SetOut(&out)
	mutation := &transactionMutationFlags{yes: true, timeout: time.Minute}
	if err := runTransactionSpamAnalyze(cmd, "org-1", 4, 100, 100, 0.5, false, mutation); err != nil {
		t.Fatal(err)
	}
	if len(bulkIgnoredIDs) != 2 || bulkIgnoredIDs[0] != "txn-single" || bulkIgnoredIDs[1] != "txn-same-token" {
		t.Fatalf("bulk ignored IDs = %#v", bulkIgnoredIDs)
	}
}

func TestNormalizedSpamSymbolsDeduplicates(t *testing.T) {
	got := normalizedSpamSymbols([]string{" tusd ", "TUSD", "eth"})
	if len(got) != 2 || got[0] != "TUSD" || got[1] != "ETH" {
		t.Fatalf("symbols = %#v", got)
	}
}
