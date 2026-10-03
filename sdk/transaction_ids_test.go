package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestTransactionSearchResultsCanBePassedUnchangedToGet(t *testing.T) {
	for _, transactionID := range []string{
		"0x" + strings.Repeat("a1", 32),
		"manual-1ec93970-54ee-4e32-a38d-ec2a042fe135",
		"import:csv:row-42",
		"import/batch-1:row-42",
		"import/batch%2F1:row?42#result",
		"4MkxKMkXFPxHRzvQtBsdn3Dn37UaVQzpcFg5SyD1wSKBNTXeAEFCjqpTBPqept9KFx9Tycto9wspspfY9MxMnBm5",
		"ETH.0xabc",
	} {
		t.Run(transactionID, func(t *testing.T) {
			// Reproduce the production shape: an Unknown transaction with no
			// lines must still be fetchable using the exact ID search returned.
			transaction := map[string]any{
				"id": transactionID, "transactionType": "Unknown", "lines": []any{},
				"categorizationStatus": "Uncategorized",
			}
			var calls []string
			options := Options{OrganizationID: "org-1", Token: "test-token", CoreBaseURL: "https://unit.invalid"}
			options.HTTPClient = &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
				call := r.Method + " " + r.URL.Path
				calls = append(calls, call)
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Fatal("request lost its invocation token")
				}
				switch call {
				case "GET /v3/orgs/org-1":
					return contractResponse(r, `{"id":"org-1","timezone":"UTC"}`), nil
				case "GET /orgs/org-1/wallets":
					return contractResponse(r, `{"items":[]}`), nil
				case "POST /v3/orgs/org-1/transactions/search":
					return contractResponse(r, string(contractJSON(t, map[string]any{"transactions": []any{transaction}}))), nil
				case "GET /v3/orgs/org-1/transactions/" + transactionID:
					if r.URL.EscapedPath() != "/v3/orgs/org-1/transactions/"+url.PathEscape(transactionID) || r.URL.RawQuery != "" || r.URL.Fragment != "" {
						t.Fatalf("transaction ID was not encoded as one path component: %s", r.URL)
					}
					return contractResponse(r, string(contractJSON(t, transaction))), nil
				default:
					return nil, fmt.Errorf("unexpected request %s", call)
				}
			})}
			client := NewClient(options)
			search, err := client.Invoke(context.Background(), Request{Operation: "bitwave_transaction_search", Arguments: json.RawMessage(`{"limit":10}`)})
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Transactions []struct {
					ID              string `json:"id"`
					TransactionType string `json:"transactionType"`
					LineCount       int    `json:"lineCount"`
				} `json:"transactions"`
			}
			if err := json.Unmarshal(search.Data, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Transactions) != 1 || result.Transactions[0].ID != transactionID || result.Transactions[0].TransactionType != "Unknown" || result.Transactions[0].LineCount != 0 {
				t.Fatalf("unexpected search result: %s", search.Output)
			}
			get, err := client.Invoke(context.Background(), Request{Operation: "bitwave_transaction_get", Arguments: contractJSON(t, map[string]any{"arguments": []string{result.Transactions[0].ID}})})
			if err != nil {
				t.Fatal(err)
			}
			var detail map[string]any
			if err := json.Unmarshal(get.Data, &detail); err != nil || detail["id"] != transactionID {
				t.Fatalf("get did not preserve search ID: %s, %v", get.Output, err)
			}
			wantCalls := []string{"GET /v3/orgs/org-1", "GET /orgs/org-1/wallets", "POST /v3/orgs/org-1/transactions/search", "GET /v3/orgs/org-1/transactions/" + transactionID}
			if !reflect.DeepEqual(calls, wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, wantCalls)
			}
		})
	}
}

func TestAdaptersPreserveSafeLocalValidationErrors(t *testing.T) {
	options := Options{OrganizationID: "org-1", Token: "test-token", HTTPClient: &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
		t.Fatal("local validation must not make an HTTP request")
		return nil, errors.New("unexpected HTTP request")
	})}}
	tests := []struct {
		operation string
		arguments string
		argv      []string
		message   string
	}{
		{"bitwave_transaction_get", `{"arguments":[" \t"]}`, []string{"transaction", "get", " \t"}, "transaction ID is required"},
		{"bitwave_transaction_search", `{"limit":101}`, []string{"transaction", "search", "--limit", "101"}, "--limit must be between 1 and 100"},
		{"bitwave_transaction_search", `{"from":"2024-01-01"}`, []string{"transaction", "search", "--from", "2024-01-01"}, "--from and --to must be supplied together"},
		{"bitwave_transaction_search", `{"sort-direction":"https://private.invalid/?token=secret"}`, []string{"transaction", "search", "--sort-direction", "https://private.invalid/?token=secret"}, "--sort-direction must be asc or desc"},
	}
	for _, test := range tests {
		t.Run(test.message, func(t *testing.T) {
			_, typed := NewClient(options).Invoke(context.Background(), Request{Operation: test.operation, Arguments: json.RawMessage(test.arguments)})
			cli := ExecuteWithOptions(context.Background(), ExecuteOptions{ClientOptions: options, Args: test.argv})
			for _, err := range []error{typed, cli.Err} {
				var validation *ValidationError
				if !errors.As(err, &validation) || validation.Error() != test.message {
					t.Fatalf("missing safe validation failure: %v", err)
				}
			}
			if cli.ExitCode == 0 || !strings.Contains(cli.Stderr, test.message) {
				t.Fatalf("terminal diagnostics changed: %+v", cli)
			}
		})
	}
}

func TestTransactionGetNetworkPrefixIsExplicitAcrossAdapters(t *testing.T) {
	for _, test := range []struct {
		name, id, network, want string
		accountingDetails       bool
	}{
		{name: "opaque", id: "0xabc", want: "0xabc"},
		{name: "explicit network", id: "0xabc", network: "ethereum", want: "ETH.0xabc"},
		{name: "already qualified", id: "BSC.0xabc", network: "ethereum", want: "BSC.0xabc"},
		{name: "accounting detail", id: "manual-123", want: "manual-123", accountingDetails: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			options := Options{OrganizationID: "org-1", Token: "test-token", CoreBaseURL: "https://unit.invalid", HTTPClient: &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				wantPath := "/v3/orgs/org-1/transactions/" + test.want
				if test.accountingDetails {
					wantPath = "/txns/org-1/" + test.want
				}
				if r.Method != http.MethodGet || r.URL.Path != wantPath {
					t.Fatalf("request = %s %s, want GET %s", r.Method, r.URL.Path, wantPath)
				}
				return contractResponse(r, string(contractJSON(t, map[string]any{"id": test.want}))), nil
			})}}
			args := map[string]any{"arguments": []string{test.id}}
			argv := []string{"transaction", "get", test.id}
			if test.network != "" {
				args["network"] = test.network
				argv = append(argv, "--network", test.network)
			}
			if test.accountingDetails {
				args["accounting-details"] = true
				argv = append(argv, "--accounting-details")
			}
			if _, err := NewClient(options).Invoke(context.Background(), Request{Operation: "bitwave_transaction_get", Arguments: contractJSON(t, args)}); err != nil {
				t.Fatal(err)
			}
			if result := ExecuteWithOptions(context.Background(), ExecuteOptions{ClientOptions: options, Args: argv}); result.Err != nil || result.ExitCode != 0 {
				t.Fatalf("CLI failed: %+v", result)
			}
			if calls != 2 {
				t.Fatalf("HTTP calls = %d, want 2", calls)
			}
		})
	}
}

func TestTransactionMutationPreviewsPreserveOpaqueIDs(t *testing.T) {
	for _, operation := range []string{"bitwave_transaction_categorize", "bitwave_transaction_review_accept", "bitwave_transaction_review_ignore"} {
		t.Run(operation, func(t *testing.T) {
			args := map[string]any{"arguments": []string{"0xabc"}, "dry-run": true}
			if operation == "bitwave_transaction_categorize" {
				args["input"] = "-"
			}
			options := Options{OrganizationID: "org-1", Token: "test-token", HTTPClient: &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
				t.Fatal("dry-run must not make an HTTP request")
				return nil, errors.New("unexpected HTTP request")
			})}}
			result, err := NewClient(options).Invoke(context.Background(), Request{
				Operation: operation, Arguments: contractJSON(t, args),
				Input: strings.NewReader(`{"type":"trade","categorizationMethod":1,"accountingConnectionId":"ac-1","exchangeRates":[],"exchangeRateVersion":0}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.Output, "0xabc") || strings.Contains(result.Output, ".0xabc") {
				t.Fatalf("opaque ID was not preserved: %s", result.Output)
			}
		})
	}
}
