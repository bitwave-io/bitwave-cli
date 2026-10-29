package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	op "github.com/bitwave-io/bitwave-cli/internal/operation"
)

// transactionsPlatform is a concrete fake of the transactions API: it answers
// GET and PATCH /v3/orgs/org-1/transactions/{id} from the IDs it holds, and
// records every request.
type transactionsPlatform struct {
	mu       sync.Mutex
	stored   map[string]string // transaction id -> JSON body
	failWith int               // non-zero answers every request with this status
	requests []string
}

func (p *transactionsPlatform) respond(r *http.Request) (*http.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	const prefix = "/v3/orgs/org-1/transactions/"
	i := strings.Index(r.URL.Path, prefix)
	if i < 0 {
		return jsonResponse(r, http.StatusTeapot, `{"message":"unexpected path"}`), nil
	}
	id := r.URL.Path[i+len(prefix):]
	p.requests = append(p.requests, r.Method+" "+id)
	if p.failWith != 0 {
		return jsonResponse(r, p.failWith, `{"message":"upstream failure"}`), nil
	}
	body, ok := p.stored[id]
	if !ok {
		return jsonResponse(r, http.StatusNotFound, `{"message":"Transaction not found"}`), nil
	}
	if r.Method == http.MethodPatch {
		return jsonResponse(r, http.StatusOK, `{}`), nil
	}
	return jsonResponse(r, http.StatusOK, body), nil
}

func invokeWithStderr(t *testing.T, runtime *op.Runtime, definition *op.Definition, args, flags []string, stdin string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	call := op.NewCall(op.WithRuntime(context.Background(), runtime), definition, strings.NewReader(stdin), &stdout, &stderr)
	if err := definition.Flags().Parse(flags); err != nil {
		t.Fatal(err)
	}
	err := definition.Invoke(call, args)
	return stdout.String(), stderr.String(), err
}

// An organization can store an explorer hash bare. When --network prefixes it
// and the prefixed ID is not found, get reads the ID exactly as given.
func TestTransactionGetReadsTheBareHashWhenThePrefixedIDIsNotFound(t *testing.T) {
	platform := &transactionsPlatform{stored: map[string]string{"0xabc": `{"id":"0xabc","methodId":"0x"}`}}
	runtime := ruleRunRuntime(t, platform.respond)
	stdout, stderr, err := invokeWithStderr(t, runtime, newGetOrgTransactionCmd(), []string{"0xabc"}, []string{"--network", "ethereum"}, "")
	if err != nil {
		t.Fatalf("%v (stderr: %s)", err, stderr)
	}
	var got struct {
		ID string `json:"id"`
	}
	if json.Unmarshal([]byte(stdout), &got) != nil || got.ID != "0xabc" {
		t.Fatalf("stdout = %q", stdout)
	}
	if want := []string{"GET ETH.0xabc", "GET 0xabc"}; strings.Join(platform.requests, ",") != strings.Join(want, ",") {
		t.Fatalf("requests = %v, want %v", platform.requests, want)
	}
	if !strings.Contains(stderr, "ETH.0xabc was not found") || !strings.Contains(stderr, "stored as 0xabc") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestTransactionGetPrefersThePrefixedIDWhenItExists(t *testing.T) {
	platform := &transactionsPlatform{stored: map[string]string{"ETH.0xabc": `{"id":"ETH.0xabc"}`, "0xabc": `{"id":"0xabc"}`}}
	runtime := ruleRunRuntime(t, platform.respond)
	stdout, stderr, err := invokeWithStderr(t, runtime, newGetOrgTransactionCmd(), []string{"0xabc"}, []string{"--network", "ethereum"}, "")
	if err != nil || !strings.Contains(stdout, `"ETH.0xabc"`) || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q err=%v", stdout, stderr, err)
	}
	if len(platform.requests) != 1 {
		t.Fatalf("requests = %v", platform.requests)
	}
}

func TestTransactionGetNamesBothIDsWhenNeitherExists(t *testing.T) {
	platform := &transactionsPlatform{stored: map[string]string{}}
	runtime := ruleRunRuntime(t, platform.respond)
	_, _, err := invokeWithStderr(t, runtime, newGetOrgTransactionCmd(), []string{"0xabc"}, []string{"--network", "ethereum"}, "")
	if err == nil || !strings.Contains(err.Error(), "not found as ETH.0xabc or as 0xabc") {
		t.Fatalf("err = %v", err)
	}
	if len(platform.requests) != 2 {
		t.Fatalf("requests = %v", platform.requests)
	}
}

// Without --network nothing was rewritten, and a failure other than 404 is not
// a missing prefix: neither reads a second ID.
func TestTransactionGetReadsOneIDOtherwise(t *testing.T) {
	for _, tc := range []struct {
		name     string
		flags    []string
		failWith int
	}{
		{"no network flag", nil, 0},
		{"already qualified", []string{"--network", "ethereum"}, 0},
		{"server error", []string{"--network", "ethereum"}, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			platform := &transactionsPlatform{stored: map[string]string{}, failWith: tc.failWith}
			runtime := ruleRunRuntime(t, platform.respond)
			id := "0xabc"
			if tc.name == "already qualified" {
				id = "BSC.0xabc"
			}
			if _, _, err := invokeWithStderr(t, runtime, newGetOrgTransactionCmd(), []string{id}, tc.flags, ""); err == nil {
				t.Fatal("expected an error")
			}
			if len(platform.requests) != 1 {
				t.Fatalf("requests = %v", platform.requests)
			}
		})
	}
}

// A mutation never retries another ID; its 404 says why it may be missing.
func TestCategorizeExplainsANetworkPrefixedNotFound(t *testing.T) {
	platform := &transactionsPlatform{stored: map[string]string{"0xabc": `{"id":"0xabc"}`}}
	runtime := ruleRunRuntime(t, platform.respond)
	body := `{"type":"trade","categorizationMethod":1,"accountingConnectionId":"ac-1","exchangeRates":[],"exchangeRateVersion":0}`
	_, _, err := invokeWithStderr(t, runtime, newCategorizeTransactionCmd(), []string{"0xabc"}, []string{"--network", "ethereum", "--input", "-", "--yes", "--json"}, body)
	if err == nil || !strings.Contains(err.Error(), "retry without --network") || !strings.Contains(err.Error(), "without a network prefix as 0xabc") {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(platform.requests, ",") != "PATCH ETH.0xabc" {
		t.Fatalf("requests = %v", platform.requests)
	}

	// Without --network the 404 is reported as it is.
	platform.requests = nil
	_, _, err = invokeWithStderr(t, runtime, newCategorizeTransactionCmd(), []string{"0xdef"}, []string{"--input", "-", "--yes", "--json"}, body)
	if err == nil || strings.Contains(err.Error(), "--network") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareRuleObjectEnabledCreateSetsDisabledFalse(t *testing.T) {
	decode := func(rule json.RawMessage) (bool, bool) {
		var prepared map[string]map[string]json.RawMessage
		if err := json.Unmarshal(rule, &prepared); err != nil {
			t.Fatal(err)
		}
		raw, ok := prepared["transfer"]["disabled"]
		var disabled bool
		_ = json.Unmarshal(raw, &disabled)
		return disabled, ok
	}
	const base = `"name":"test","priority":1,"accountingConnectionId":"ac-1","action":{"type":"Ignore"}`
	rule, err := prepareRuleObject(json.RawMessage(`{"transfer":{`+base+`}}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if disabled, present := decode(rule); !present || disabled {
		t.Fatalf("--enabled without `disabled` must send disabled:false: %s", rule)
	}
	// --enabled only permits an enabled rule: an explicit disabled:true stays.
	rule, err = prepareRuleObject(json.RawMessage(`{"transfer":{`+base+`,"disabled":true}}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if disabled, _ := decode(rule); !disabled {
		t.Fatalf("explicit disabled:true was overridden: %s", rule)
	}
}

func TestRuleUpdateRequiresDisabled(t *testing.T) {
	platform := &transactionsPlatform{stored: map[string]string{}}
	runtime := ruleRunRuntime(t, platform.respond)
	_, _, err := invokeWithStderr(t, runtime, newUpdateRawRuleCmd(), []string{"rule-1"}, []string{"--input", "-", "--dry-run", "--json"},
		`{"transfer":{"name":"test","priority":1,"accountingConnectionId":"ac-1","action":{"type":"Ignore"}}}`)
	if err == nil || !strings.Contains(err.Error(), "requires a boolean `disabled`") {
		t.Fatalf("err = %v", err)
	}
	if len(platform.requests) != 0 {
		t.Fatalf("an update without `disabled` reached the platform: %v", platform.requests)
	}

	stdout, _, err := invokeWithStderr(t, runtime, newUpdateRawRuleCmd(), []string{"rule-1"}, []string{"--input", "-", "--dry-run", "--json"},
		`{"transfer":{"name":"test","priority":1,"accountingConnectionId":"ac-1","action":{"type":"Ignore"},"disabled":false}}`)
	if err != nil || !strings.Contains(stdout, `"preview"`) {
		t.Fatalf("stdout=%q err=%v", stdout, err)
	}
}
