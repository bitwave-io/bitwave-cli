package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	op "github.com/bitwave-io/bitwave-cli/internal/operation"
)

// spamTestCall builds an invocation-scoped call bound to org-1. Endpoints and
// credentials come from options, exactly as a hosted caller supplies them.
func spamTestCall(t *testing.T, opts op.Options, out io.Writer) *op.Call {
	t.Helper()
	opts.OrganizationID = "org-1"
	opts.Token = "token"
	opts.WorkingDirectory = t.TempDir()
	rt, err := op.NewRuntime(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return op.NewCall(op.WithRuntime(context.Background(), rt), &op.Definition{Use: "spam"}, nil, out, io.Discard)
}

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
		case "/orgs/org-1/lookups":
			if r.URL.Query().Get("fieldName") != "amountCurrencyName" || r.URL.Query().Get("limit") != "-1" {
				t.Fatalf("lookup query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"values":["SPAM"]}`))
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
			if len(body.Filters.CategorizationStatuses) != 1 || body.Filters.CategorizationStatuses[0] != "Uncategorized" || len(body.Filters.IgnoredStatuses) != 1 {
				t.Fatalf("transaction scope = %#v", body.Filters)
			}
			_, _ = w.Write([]byte(`{"transactions":[
				{"id":"txn-single","categorizationStatus":"Uncategorized","ignored":false,"lines":[{"line":0,"amountCurrencyId":"COIN.999","amountCurrencyName":"SPAM"}]},
				{"id":"txn-same-token","categorizationStatus":"Uncategorized","ignored":false,"lines":[{"line":0,"amountCurrencyId":"COIN.999"},{"line":1,"amountCurrencyId":"COIN.999"}]},
				{"id":"txn-trade","categorizationStatus":"Uncategorized","ignored":false,"lines":[{"line":0,"amountCurrencyId":"COIN.999"},{"line":1,"amountCurrencyId":"COIN.10"}]}
			]}`))
		case "/v3/orgs/org-1/transactions/bulk-state":
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

	var out bytes.Buffer
	cmd := spamTestCall(t, op.Options{CoreBaseURL: coreServer.URL, TransactionsBaseURL: coreServer.URL, AddressBaseURL: addressServer.URL}, &out)
	if err := runTransactionSpamAnalyze(cmd, "org-1", nil, 4, 100, 100, 0.5, false, nil); err != nil {
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

	out.Reset()
	cmd = spamTestCall(t, op.Options{CoreBaseURL: coreServer.URL, TransactionsBaseURL: coreServer.URL, AddressBaseURL: addressServer.URL}, &out)
	mutation := &transactionMutationFlags{yes: true, timeout: time.Minute}
	if err := runTransactionSpamAnalyze(cmd, "org-1", nil, 4, 100, 100, 0.5, false, mutation); err != nil {
		t.Fatal(err)
	}
	if len(bulkIgnoredIDs) != 2 || bulkIgnoredIDs[0] != "txn-single" || bulkIgnoredIDs[1] != "txn-same-token" {
		t.Fatalf("bulk ignored IDs = %#v", bulkIgnoredIDs)
	}
}

func TestSelectedTickerIgnoreUsesUIFilterAndUncategorizedScope(t *testing.T) {
	var bulkIgnoredIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/symbols/ZEPE.IO":
			_, _ = w.Write([]byte(`{"coinId":999,"networkId":"polygon","symbol":"ZEPE.IO","spamScore":0.9}`))
		case "/v3/orgs/org-1/transactions/search":
			var body struct {
				Filters struct {
					AmountCurrencyNames    []string `json:"amountCurrencyNames"`
					CategorizationStatuses []string `json:"categorizationStatuses"`
					IgnoredStatuses        []string `json:"ignoredStatuses"`
				} `json:"filters"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Filters.AmountCurrencyNames) != 1 || body.Filters.AmountCurrencyNames[0] != "ZEPE.IO" {
				t.Fatalf("ticker filter = %#v", body.Filters.AmountCurrencyNames)
			}
			if len(body.Filters.CategorizationStatuses) != 1 || body.Filters.CategorizationStatuses[0] != "Uncategorized" || len(body.Filters.IgnoredStatuses) != 1 || body.Filters.IgnoredStatuses[0] != "Unignored" {
				t.Fatalf("transaction scope = %#v", body.Filters)
			}
			_, _ = w.Write([]byte(`{"transactions":[
				{"id":"txn-spam-only","lines":[{"amountCurrencyName":"Zepe.io"}]},
				{"id":"txn-mixed","lines":[{"amountCurrencyName":"Zepe.io"},{"amountCurrencyName":"ETH"}]}
			]}`))
		case "/v3/orgs/org-1/transactions/bulk-state":
			var body struct {
				TransactionIDs []string `json:"transactionIds"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			bulkIgnoredIDs = append([]string(nil), body.TransactionIDs...)
			_, _ = w.Write([]byte(`{"success":true,"processed":1,"successCount":1,"failed":[]}`))
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := spamTestCall(t, op.Options{CoreBaseURL: server.URL, TransactionsBaseURL: server.URL, AddressBaseURL: server.URL}, &out)
	mutation := &transactionMutationFlags{yes: true, timeout: time.Minute}
	if err := runTransactionSpamAnalyze(cmd, "org-1", []string{"Zepe.io"}, 4, 100, 100, 0.5, true, mutation); err != nil {
		t.Fatal(err)
	}
	if len(bulkIgnoredIDs) != 1 || bulkIgnoredIDs[0] != "txn-spam-only" {
		t.Fatalf("bulk ignored IDs = %#v", bulkIgnoredIDs)
	}
	var result struct {
		TransactionScope string `json:"transactionScope"`
		IgnoreReadyCount int    `json:"ignoreReadyCount"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.TransactionScope != "uncategorized-only" || result.IgnoreReadyCount != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestSelectedCleanTickerCannotReachTransactionIgnore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/symbols/BSC_USDT" {
			t.Fatalf("clean ticker unexpectedly reached %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"coinId":168107,"networkId":"bsc","symbol":"BSC_USDT"}`))
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := spamTestCall(t, op.Options{CoreBaseURL: server.URL, TransactionsBaseURL: server.URL, AddressBaseURL: server.URL}, &out)
	mutation := &transactionMutationFlags{yes: true, timeout: time.Minute}
	if err := runTransactionSpamAnalyze(cmd, "org-1", []string{"BSC_USDT"}, 4, 100, 100, 0.5, false, mutation); err != nil {
		t.Fatal(err)
	}
	var result struct {
		ConfirmedSpamTickers []string `json:"confirmedSpamTickers"`
		IgnoreReadyCount     int      `json:"ignoreReadyCount"`
		BulkIgnore           struct {
			Status    string `json:"status"`
			Processed int    `json:"processed"`
		} `json:"bulkIgnore"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.ConfirmedSpamTickers) != 0 || result.IgnoreReadyCount != 0 || result.BulkIgnore.Status != "noop" || result.BulkIgnore.Processed != 0 {
		t.Fatalf("clean ticker result = %#v", result)
	}
}

func TestNormalizedSpamSymbolsDeduplicates(t *testing.T) {
	got := normalizedSpamSymbols([]string{" tusd ", "TUSD", "eth"})
	if len(got) != 2 || got[0] != "TUSD" || got[1] != "ETH" {
		t.Fatalf("symbols = %#v", got)
	}
}

func TestSpamCheckReadsSymbolsFromStdinAndClassifiesScores(t *testing.T) {
	address := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/symbols/SPAM":
			_, _ = w.Write([]byte(`{"coinId":9,"symbol":"SPAM","spamScore":0.9}`))
		case "/symbols/DUBIOUS":
			_, _ = w.Write([]byte(`{"coinId":8,"symbol":"DUBIOUS","spamScore":0.2}`))
		case "/symbols/TUSD":
			_, _ = w.Write([]byte(`{"coinId":7,"symbol":"TUSD"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer address.Close()

	var out bytes.Buffer
	call := spamTestCall(t, op.Options{AddressBaseURL: address.URL}, &out)
	call.Input = strings.NewReader("spam, dubious\ntusd\nmissing\n")
	definition := newTransactionSpamCheckCmd()
	call.Definition = definition
	if err := definition.Flags().Parse([]string{"--input", "-"}); err != nil {
		t.Fatal(err)
	}
	if err := definition.Invoke(call, nil); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Source  string `json:"source"`
		Results []struct {
			RequestedSymbol      string `json:"requestedSymbol"`
			Status               string `json:"status"`
			IgnoreRecommendation bool   `json:"ignoreRecommendation"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output = %s err=%v", out.String(), err)
	}
	got := map[string]string{}
	for _, r := range result.Results {
		got[r.RequestedSymbol] = r.Status
		if r.IgnoreRecommendation != (r.Status == "spam") {
			t.Fatalf("%s: ignoreRecommendation = %v with status %s", r.RequestedSymbol, r.IgnoreRecommendation, r.Status)
		}
	}
	want := map[string]string{"SPAM": "spam", "DUBIOUS": "review", "TUSD": "clean", "MISSING": "unresolved"}
	for symbol, status := range want {
		if got[symbol] != status {
			t.Fatalf("statuses = %v, want %v", got, want)
		}
	}
	if result.Source != address.URL {
		t.Fatalf("source = %q, want the configured address service %q", result.Source, address.URL)
	}
}

func TestSpamCheckRequiresSymbols(t *testing.T) {
	var out bytes.Buffer
	call := spamTestCall(t, op.Options{}, &out)
	definition := newTransactionSpamCheckCmd()
	call.Definition = definition
	if err := definition.Invoke(call, nil); err == nil || !strings.Contains(err.Error(), "at least one symbol") {
		t.Fatalf("err = %v", err)
	}
}

func TestSpamSymbolInputIsScopedAndBounded(t *testing.T) {
	call := spamTestCall(t, op.Options{}, io.Discard)
	if _, err := readSpamSymbols(call.Context(), "/etc/hosts", nil); err == nil {
		t.Fatal("a hosted invocation must not read arbitrary filesystem paths")
	}
	oversized := strings.NewReader(strings.Repeat("A", spamSymbolInputLimit+1))
	if _, err := readSpamSymbols(call.Context(), "-", oversized); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v", err)
	}
}

func TestAddressClientUsesCallerEndpointNotEnvironment(t *testing.T) {
	t.Setenv("BITWAVE_ADDRESS_SERVICE_URL", "https://environment.invalid")
	ctx := spamTestCall(t, op.Options{}, io.Discard).Context()
	if got := newAddressClient(ctx).BaseURL; got != "https://address-svc-utyjy373hq-uc.a.run.app" {
		t.Fatalf("default base = %q; the process environment must never choose the endpoint", got)
	}
	ctx = spamTestCall(t, op.Options{AddressBaseURL: "https://address.example/"}, io.Discard).Context()
	if got := newAddressClient(ctx).BaseURL; got != "https://address.example" {
		t.Fatalf("configured base = %q", got)
	}
}

func TestSpamBulkIgnoreRefusesWithoutConfirmationAndSendsNoMutation(t *testing.T) {
	mutated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/symbols/SPAM":
			_, _ = w.Write([]byte(`{"coinId":999,"symbol":"SPAM","spamScore":0.9}`))
		case "/v3/orgs/org-1/transactions/search":
			_, _ = w.Write([]byte(`{"transactions":[{"id":"txn-spam-only","lines":[{"amountCurrencyName":"SPAM"}]}]}`))
		case "/v3/orgs/org-1/transactions/bulk-state":
			mutated = true
			_, _ = w.Write([]byte(`{"success":true,"processed":1,"successCount":1,"failed":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	call := spamTestCall(t, op.Options{CoreBaseURL: server.URL, TransactionsBaseURL: server.URL, AddressBaseURL: server.URL}, &out)
	definition := newTransactionSpamBulkIgnoreCmd()
	call.Definition = definition
	if err := definition.Flags().Parse([]string{"--ticker", "SPAM"}); err != nil {
		t.Fatal(err)
	}
	err := definition.Invoke(call, nil)
	if err == nil || !strings.Contains(err.Error(), "without --yes") {
		t.Fatalf("err = %v, want a refusal to bulk-ignore without --yes", err)
	}
	if mutated {
		t.Fatal("bulk-state was called without confirmation")
	}
}

func TestSpamBulkIgnoreRejectsThresholdBelowDefault(t *testing.T) {
	var out bytes.Buffer
	call := spamTestCall(t, op.Options{CoreBaseURL: "http://127.0.0.1:1", TransactionsBaseURL: "http://127.0.0.1:1", AddressBaseURL: "http://127.0.0.1:1"}, &out)
	definition := newTransactionSpamBulkIgnoreCmd()
	call.Definition = definition
	if err := definition.Flags().Parse([]string{"--ticker", "SPAM", "--threshold", "0", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if err := definition.Invoke(call, nil); err == nil || !strings.Contains(err.Error(), "at least 0.5") {
		t.Fatalf("err = %v, want bulk-ignore to refuse a threshold that turns any scored token into spam", err)
	}
}

func TestSpamAnalyzeStillAllowsLowerThresholdForReview(t *testing.T) {
	address := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"coinId":5,"symbol":"DUBIOUS","spamScore":0.2}`))
	}))
	defer address.Close()
	var out bytes.Buffer
	call := spamTestCall(t, op.Options{CoreBaseURL: address.URL, TransactionsBaseURL: address.URL, AddressBaseURL: address.URL}, &out)
	definition := newTransactionSpamAnalyzeCmd()
	call.Definition = definition
	if err := definition.Flags().Parse([]string{"--ticker", "DUBIOUS", "--threshold", "0.1"}); err != nil {
		t.Fatal(err)
	}
	if err := definition.Invoke(call, nil); err != nil && strings.Contains(err.Error(), "at least") {
		t.Fatalf("analyze is read-only and must accept a lower review threshold: %v", err)
	}
}
