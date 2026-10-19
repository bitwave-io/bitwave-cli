package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	op "github.com/bitwave-io/bitwave-cli/internal/operation"
)

// fakeTreasury is a concrete in-memory implementation of the treasury-svc and
// analytics-query-svc routes as exposed through the API gateway. It records
// every request so tests can assert method, path, query, and body.
type fakeTreasury struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
	monitors map[string]map[string]any
	order    []string
	alerts   []map[string]any
	channels []map[string]any
	fail     map[string]int // "METHOD path" -> status to return
	nextID   int
}

type recordedRequest struct {
	Method, Path, RawQuery, Authorization string
	Body                                  []byte
}

func (r recordedRequest) JSON(t *testing.T) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatalf("request body %q: %v", r.Body, err)
	}
	return body
}

const treasuryTestOrg = "org-1"

func newFakeTreasury(t *testing.T) *fakeTreasury {
	f := &fakeTreasury{t: t, monitors: map[string]map[string]any{}, fail: map[string]int{}}
	f.addMonitor(map[string]any{"id": "mon-static", "orgId": treasuryTestOrg, "walletId": "w-1", "walletName": "Ops ETH", "network": "eth", "address": "0xabc",
		"currencyId": 10, "currencyType": "Coin", "approaching": map[string]any{"mode": "STATIC", "value": 5}, "breaching": map[string]any{"mode": "STATIC", "value": 2},
		"alertEnabled": true, "notifyOn": []any{"APPROACHING", "BREACHING"}, "lastStatus": "HEALTHY", "lastBalance": 7.5})
	f.addMonitor(map[string]any{"id": "mon-dynamic", "orgId": treasuryTestOrg, "walletId": "w-2", "walletName": "Canton validator", "network": "canton",
		"currencyId": 566847868622592, "currencyType": "Coin", "approaching": map[string]any{"mode": "STATIC", "value": 100},
		"breaching": map[string]any{"mode": "DYNAMIC", "value": 0, "rule": map[string]any{"templateId": "tpl-1", "parameters": map[string]any{"party": "validator::1"},
			"predicate": "$balance < $total_earned * 0.702", "lastResult": false, "lastEvaluatedAt": "2026-09-30T00:00:00Z", "lastError": "stale"}},
		"alertEnabled": true})
	f.alerts = []map[string]any{
		{"id": "al-1", "configId": "mon-static", "walletName": "Ops ETH", "tier": "BREACHING", "status": "ACTIVE", "balance": 1.5, "threshold": 2, "firedAt": "2026-09-30T07:30:00Z"},
		{"id": "al-2", "configId": "mon-static", "walletName": "Ops ETH", "tier": "APPROACHING", "status": "RESOLVED", "balance": 4, "threshold": 5, "firedAt": "2026-09-29T07:30:00Z"},
	}
	f.channels = []map[string]any{{"channel": "EMAIL", "enabled": true, "target": "ops@example.com", "updatedAt": "2026-09-29T16:04:16Z"}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeTreasury) addMonitor(m map[string]any) {
	f.monitors[m["id"].(string)] = m
	f.order = append(f.order, m["id"].(string))
}

func (f *fakeTreasury) write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func (f *fakeTreasury) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery, Authorization: r.Header.Get("Authorization"), Body: body})
	if status, ok := f.fail[r.Method+" "+r.URL.Path]; ok {
		f.write(w, status, map[string]string{"error": "canned failure"})
		return
	}
	prefix := "/v3/orgs/" + treasuryTestOrg + "/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		f.write(w, http.StatusNotFound, map[string]string{"error": "unknown org route"})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix), "/")
	route := r.Method + " " + parts[0]
	switch {
	case route == "GET monitoring-configs" && len(parts) == 1:
		list := make([]map[string]any, 0, len(f.order))
		for _, id := range f.order {
			list = append(list, f.monitors[id])
		}
		f.write(w, http.StatusOK, list)
	case route == "POST monitoring-configs" && len(parts) == 1:
		var created map[string]any
		_ = json.Unmarshal(body, &created)
		f.nextID++
		created["id"] = "mon-new-" + string(rune('0'+f.nextID))
		f.addMonitor(created)
		f.write(w, http.StatusCreated, created)
	case parts[0] == "monitoring-configs" && len(parts) >= 2:
		m, ok := f.monitors[parts[1]]
		if !ok {
			f.write(w, http.StatusNotFound, map[string]string{"error": "monitoring config not found"})
			return
		}
		switch {
		case r.Method == http.MethodGet && len(parts) == 2:
			f.write(w, http.StatusOK, m)
		case r.Method == http.MethodGet && len(parts) == 3 && parts[2] == "balance-history":
			f.write(w, http.StatusOK, map[string]any{"currencyId": m["currencyId"], "currencyType": m["currencyType"], "points": []any{
				map[string]any{"day": "2026-09-29", "balance": 7.25}, map[string]any{"day": "2026-09-30", "balance": 7.5}}})
		case r.Method == http.MethodPut && len(parts) == 2:
			var update map[string]any
			_ = json.Unmarshal(body, &update)
			for key, value := range update {
				m[key] = value
			}
			m["lastStatus"] = "APPROACHING"
			f.write(w, http.StatusOK, m)
		case r.Method == http.MethodDelete && len(parts) == 2:
			delete(f.monitors, parts[1])
			f.write(w, http.StatusNoContent, nil)
		default:
			f.write(w, http.StatusMethodNotAllowed, nil)
		}
	case route == "GET alerts":
		out := []map[string]any{}
		for _, a := range f.alerts {
			status := r.URL.Query().Get("status")
			if status != "" && !(status == "OPEN" && a["status"] != "RESOLVED") && a["status"] != status {
				continue
			}
			if tier := r.URL.Query().Get("tier"); tier != "" && a["tier"] != tier {
				continue
			}
			out = append(out, a)
		}
		f.write(w, http.StatusOK, out)
	case route == "POST alerts" && len(parts) == 3 && parts[2] == "acknowledge":
		for _, a := range f.alerts {
			if a["id"] == parts[1] {
				if a["status"] == "RESOLVED" {
					f.write(w, http.StatusConflict, map[string]string{"error": "alert is resolved"})
					return
				}
				a["status"] = "ACKNOWLEDGED"
				f.write(w, http.StatusOK, a)
				return
			}
		}
		f.write(w, http.StatusNotFound, map[string]string{"error": "alert not found"})
	case route == "GET alert-channels":
		f.write(w, http.StatusOK, map[string]any{"channels": f.channels})
	case route == "PUT alert-channels":
		var put struct {
			Channels []map[string]any `json:"channels"`
		}
		_ = json.Unmarshal(body, &put)
		f.channels = put.Channels
		f.write(w, http.StatusOK, map[string]any{"channels": f.channels})
	case route == "GET analytics" && len(parts) == 2 && parts[1] == "templates":
		templates := []map[string]any{{"id": "tpl-1", "name": "Canton earned", "costTier": "small", "tags": []any{"treasury"},
			"parameters": []any{map[string]any{"name": "party", "type": "STRING", "required": true}}, "outputColumns": []any{map[string]any{"name": "total_earned", "type": "NUMBER"}}}}
		if tags := r.URL.Query().Get("tags"); tags != "" && tags != "treasury" {
			templates = []map[string]any{}
		}
		f.write(w, http.StatusOK, templates)
	case route == "GET analytics" && len(parts) == 3 && parts[1] == "templates":
		f.write(w, http.StatusOK, map[string]any{"id": parts[2], "name": "Canton earned", "sql": "SELECT 1"})
	case route == "POST analytics" && len(parts) == 3 && parts[1] == "queries" && parts[2] == "execute":
		f.write(w, http.StatusOK, map[string]any{"execution": map[string]any{"id": "exec-1", "status": "completed"},
			"result": map[string]any{"columns": []any{map[string]any{"name": "total_earned", "type": "FLOAT64"}}, "rows": []any{[]any{1234.5}}, "totalRows": 1}})
	default:
		f.write(w, http.StatusNotFound, map[string]string{"error": "unknown route " + route})
	}
}

func (f *fakeTreasury) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest(nil), f.requests...)
}

func (f *fakeTreasury) only(t *testing.T, method string) []recordedRequest {
	t.Helper()
	var out []recordedRequest
	for _, r := range f.recorded() {
		if r.Method == method {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeTreasury) expectNoRequests(t *testing.T) {
	t.Helper()
	if requests := f.recorded(); len(requests) != 0 {
		t.Fatalf("expected no network requests, got %+v", requests)
	}
}

// run invokes one operation definition against the fake server with a fresh
// runtime, exactly as the SDK adapter would.
func (f *fakeTreasury) run(t *testing.T, definition *op.Definition, stdin string, argv ...string) (string, error) {
	t.Helper()
	runtime, err := op.NewRuntime(op.Options{WorkingDirectory: t.TempDir(), OrganizationID: treasuryTestOrg, Token: "test-token", CoreBaseURL: f.server.URL, HTTPClient: f.server.Client(), UnrestrictedFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	if err := definition.Flags().Parse(argv); err != nil {
		return "", err
	}
	var out bytes.Buffer
	call := op.NewCall(op.WithRuntime(context.Background(), runtime), definition, strings.NewReader(stdin), &out, io.Discard)
	err = definition.Invoke(call, definition.Flags().Args())
	return out.String(), err
}

func decodeEnvelope(t *testing.T, output string) mutationEnvelope {
	t.Helper()
	var envelope mutationEnvelope
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode envelope %q: %v", output, err)
	}
	return envelope
}

func decodeObject(t *testing.T, output string) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal([]byte(output), &object); err != nil {
		t.Fatalf("decode %q: %v", output, err)
	}
	return object
}

func requireErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want it to contain %q", err, want)
	}
}

// ---- monitors: reads -------------------------------------------------------

func TestTreasuryMonitorsListJSONAndTable(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryMonitorsListCmd(), "", "--json")
	if err != nil {
		t.Fatal(err)
	}
	object := decodeObject(t, out)
	if object["count"] != float64(2) || object["organization"] != treasuryTestOrg || object["schemaVersion"] != "1" {
		t.Fatalf("list envelope = %v", object)
	}
	requests := f.recorded()
	if len(requests) != 1 || requests[0].Method != http.MethodGet || requests[0].Path != "/v3/orgs/org-1/monitoring-configs" || requests[0].Authorization != "Bearer test-token" {
		t.Fatalf("requests = %+v", requests)
	}

	table, err := f.run(t, newTreasuryMonitorsListCmd(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ID", "mon-static", "Ops ETH", "COIN.10", "HEALTHY", "7.5", "DYNAMIC($balance < $total_earned * 0.702)", "PENDING"} {
		if !strings.Contains(table, want) {
			t.Fatalf("table missing %q:\n%s", want, table)
		}
	}
}

func TestTreasuryMonitorsListRejectsNonArray(t *testing.T) {
	f := newFakeTreasury(t)
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"html":"catch-all"}`)) })
	_, err := f.run(t, newTreasuryMonitorsListCmd(), "")
	requireErrorContains(t, err, "expected a JSON array")
}

func TestTreasuryMonitorGetPrintsBackendJSON(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryMonitorGetCmd(), "", "mon-static")
	if err != nil {
		t.Fatal(err)
	}
	if decodeObject(t, out)["walletName"] != "Ops ETH" {
		t.Fatalf("get output = %s", out)
	}
	if got := f.recorded()[0].Path; got != "/v3/orgs/org-1/monitoring-configs/mon-static" {
		t.Fatalf("path = %s", got)
	}
	_, err = f.run(t, newTreasuryMonitorGetCmd(), "", "missing")
	requireErrorContains(t, err, "404")
}

func TestTreasuryMonitorHistory(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryMonitorHistoryCmd(), "", "mon-static", "--days", "30", "--json")
	if err != nil {
		t.Fatal(err)
	}
	request := f.recorded()[0]
	if request.Path != "/v3/orgs/org-1/monitoring-configs/mon-static/balance-history" || request.RawQuery != "days=30" {
		t.Fatalf("request = %+v", request)
	}
	if object := decodeObject(t, out); object["days"] != float64(30) || object["monitorId"] != "mon-static" {
		t.Fatalf("history = %v", object)
	}
	table, err := f.run(t, newTreasuryMonitorHistoryCmd(), "", "mon-static")
	if err != nil || !strings.Contains(table, "2026-09-30") || !strings.Contains(table, "7.25") {
		t.Fatalf("table = %q err=%v", table, err)
	}
	if f.recorded()[1].RawQuery != "days=14" {
		t.Fatalf("default days query = %q", f.recorded()[1].RawQuery)
	}
}

func TestTreasuryMonitorHistoryValidatesDaysWithoutNetwork(t *testing.T) {
	for _, days := range []string{"0", "91"} {
		f := newFakeTreasury(t)
		_, err := f.run(t, newTreasuryMonitorHistoryCmd(), "", "mon-static", "--days", days)
		requireErrorContains(t, err, "--days must be between 1 and 90")
		f.expectNoRequests(t)
	}
}

// ---- monitors: create ------------------------------------------------------

func TestTreasuryMonitorCreateStaticSendsExactBody(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryMonitorCreateCmd(), "", "--wallet", "w-9", "--wallet-name", "Ops USDC", "--network", "eth", "--address", "0xdef",
		"--currency-id", "42", "--currency-type", "coin", "--approaching", "0", "--breaching", "0", "--json", "--yes")
	requireErrorContains(t, err, "breaching threshold must be strictly less than approaching")
	f.expectNoRequests(t)

	out, err = f.run(t, newTreasuryMonitorCreateCmd(), "", "--wallet", "w-9", "--wallet-name", "Ops USDC", "--network", "eth", "--address", "0xdef",
		"--currency-id", "42", "--currency-type", "coin", "--approaching", "10", "--breaching", "0", "--json", "--yes")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	posts := f.only(t, http.MethodPost)
	if len(posts) != 1 || posts[0].Path != "/v3/orgs/org-1/monitoring-configs" {
		t.Fatalf("posts = %+v", posts)
	}
	want := map[string]any{"walletId": "w-9", "walletName": "Ops USDC", "network": "eth", "address": "0xdef", "currencyId": float64(42), "currencyType": "Coin",
		"alertEnabled": true, "notifyOn": []any{"APPROACHING", "BREACHING"},
		"approaching": map[string]any{"mode": "STATIC", "value": float64(10)}, "breaching": map[string]any{"mode": "STATIC", "value": float64(0)}}
	if got := posts[0].JSON(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %#v\nwant %#v", got, want)
	}
	envelope := decodeEnvelope(t, out)
	if envelope.Status != "success" || envelope.Operation != "create-treasury-monitor" || envelope.Result.(map[string]any)["id"] != "mon-new-1" {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestTreasuryMonitorCreateHumanOutputAndNotifyOverride(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryMonitorCreateCmd(), "", "--wallet", "w-9", "--currency-id", "42", "--approaching", "10", "--breaching", "5",
		"--alert-enabled=false", "--notify-on", "breaching,Breaching", "--currency-type", "FIAT", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if out != "created treasury monitor mon-new-1\n" {
		t.Fatalf("human output = %q", out)
	}
	body := f.only(t, http.MethodPost)[0].JSON(t)
	if body["alertEnabled"] != false || !reflect.DeepEqual(body["notifyOn"], []any{"BREACHING"}) || body["currencyType"] != "Fiat" {
		t.Fatalf("body = %v", body)
	}
}

func TestTreasuryMonitorCreateDynamicTier(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryMonitorCreateCmd(), "", "--wallet", "w-2", "--currency-id", "566847868622592", "--approaching", "100",
		"--breaching-template", "tpl-1", "--breaching-param", "party=validator::1", "--breaching-param", "limit=10", "--breaching-param", `code="10"`,
		"--breaching-param", "strict=true", "--breaching-predicate", " $balance < $total_earned * 0.702 ", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	f.expectNoRequests(t)
	envelope := decodeEnvelope(t, out)
	request := envelope.Request.(map[string]any)
	if request["method"] != "POST" || request["url"] != f.server.URL+"/v3/orgs/org-1/monitoring-configs" || !envelope.DryRun || envelope.Status != "preview" {
		t.Fatalf("preview = %#v", envelope)
	}
	breaching := request["body"].(map[string]any)["breaching"].(map[string]any)
	want := map[string]any{"mode": "DYNAMIC", "value": float64(0), "rule": map[string]any{"templateId": "tpl-1", "predicate": "$balance < $total_earned * 0.702",
		"parameters": map[string]any{"party": "validator::1", "limit": float64(10), "code": "10", "strict": true}}}
	if !reflect.DeepEqual(breaching, want) {
		t.Fatalf("breaching = %#v\nwant %#v", breaching, want)
	}
}

func TestTreasuryMonitorCreateRequiresConfirmation(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryMonitorCreateCmd(), "", "--wallet", "w-9", "--currency-id", "42", "--approaching", "10", "--breaching", "5", "--json")
	requireErrorContains(t, err, "without --yes")
	f.expectNoRequests(t)
	envelope := decodeEnvelope(t, out)
	if envelope.Status != "error" || envelope.Error == nil || envelope.Error.Code != "confirmation_required" {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestTreasuryMonitorCreateValidationNeverReachesNetwork(t *testing.T) {
	base := []string{"--wallet", "w-9", "--currency-id", "42"}
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing approaching", append(base, "--breaching", "1"), "an approaching threshold is required"},
		{"missing breaching", append(base, "--approaching", "1"), "a breaching threshold is required"},
		{"negative", append(base, "--approaching", "5", "--breaching", "-1"), "breaching threshold must be zero or greater"},
		{"order", append(base, "--approaching", "5", "--breaching", "5"), "strictly less than approaching"},
		{"missing wallet", []string{"--currency-id", "42", "--approaching", "5", "--breaching", "1"}, "walletId is required"},
		{"missing currency", []string{"--wallet", "w", "--approaching", "5", "--breaching", "1"}, "currencyId is required"},
		{"bad currency type", append(base, "--currency-type", "token", "--approaching", "5", "--breaching", "1"), "currencyType must be Coin or Fiat"},
		{"bad notify", append(base, "--notify-on", "HEALTHY", "--approaching", "5", "--breaching", "1"), "notifyOn must be one of"},
		{"empty notify", append(base, "--notify-on", "", "--approaching", "5", "--breaching", "1"), "at least one of APPROACHING or BREACHING"},
		{"dynamic without predicate", append(base, "--approaching", "5", "--breaching-template", "tpl-1"), "breaching threshold rule needs a predicate"},
		{"param without template", append(base, "--approaching", "5", "--breaching-param", "a=1", "--breaching-predicate", "$balance < 1"), "--breaching-param requires --breaching-template"},
		{"bad param", append(base, "--approaching", "5", "--breaching-template", "t", "--breaching-param", "=1", "--breaching-predicate", "$balance < 1"), "must be key=value"},
		{"duplicate param", append(base, "--approaching", "5", "--breaching-template", "t", "--breaching-param", "a=1", "--breaching-param", "a=2", "--breaching-predicate", "$balance < 1"), "duplicate parameter"},
		{"input and flags", append(base, "--input", "body.json"), "use either --input or body flags"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeTreasury(t)
			_, err := f.run(t, newTreasuryMonitorCreateCmd(), "", append(tc.args, "--yes")...)
			requireErrorContains(t, err, tc.want)
			f.expectNoRequests(t)
		})
	}
}

func TestTreasuryMonitorCreateFromInput(t *testing.T) {
	f := newFakeTreasury(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "monitor.json")
	if err := os.WriteFile(path, []byte(`{"walletId":"w-5","currencyId":10,"currencyType":"Coin","alertEnabled":true,
		"approaching":{"mode":"static","value":3},"breaching":{"value":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.run(t, newTreasuryMonitorCreateCmd(), "", "--input", path, "--yes"); err != nil {
		t.Fatal(err)
	}
	body := f.only(t, http.MethodPost)[0].JSON(t)
	if !reflect.DeepEqual(body["breaching"], map[string]any{"mode": "STATIC", "value": float64(1)}) || body["notifyOn"] != nil {
		t.Fatalf("body = %v", body)
	}

	for name, input := range map[string]string{
		"missing value":   `{"walletId":"w","currencyId":1,"currencyType":"Coin","approaching":{"mode":"STATIC","value":3},"breaching":{"mode":"STATIC"}}`,
		"unknown field":   `{"walletId":"w","currencyId":1,"currencyType":"Coin","approaching":{"value":3},"breaching":{"value":1},"lastStatus":"HEALTHY"}`,
		"two objects":     `{"walletId":"w"} {"walletId":"x"}`,
		"bad mode":        `{"walletId":"w","currencyId":1,"currencyType":"Coin","approaching":{"mode":"FLOAT","value":3},"breaching":{"value":1}}`,
		"static w/ rule":  `{"walletId":"w","currencyId":1,"currencyType":"Coin","approaching":{"value":3,"rule":{"predicate":"$balance < 1"}},"breaching":{"value":1}}`,
		"dynamic no rule": `{"walletId":"w","currencyId":1,"currencyType":"Coin","approaching":{"mode":"DYNAMIC"},"breaching":{"value":1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeTreasury(t)
			_, err := f.run(t, newTreasuryMonitorCreateCmd(), input, "--input", "-", "--yes")
			if err == nil {
				t.Fatal("expected validation error")
			}
			f.expectNoRequests(t)
		})
	}
	_, err := f.run(t, newTreasuryMonitorCreateCmd(), "", "--input", filepath.Join(dir, "missing.json"), "--yes")
	requireErrorContains(t, err, "read --input")
}

func TestTreasuryMonitorCreateSurfacesBackendValidation(t *testing.T) {
	f := newFakeTreasury(t)
	f.fail["POST /v3/orgs/org-1/monitoring-configs"] = http.StatusBadRequest
	out, err := f.run(t, newTreasuryMonitorCreateCmd(), "", "--wallet", "w", "--currency-id", "1", "--approaching", "2", "--breaching", "1", "--yes", "--json")
	requireErrorContains(t, err, "400")
	if envelope := decodeEnvelope(t, out); envelope.Error == nil || envelope.Error.Code != "api_error" || envelope.Error.HTTPStatus != 400 {
		t.Fatalf("envelope = %#v", envelope)
	}
}

// ---- monitors: update / delete ---------------------------------------------

func TestTreasuryMonitorUpdateSendsOnlyChangedFields(t *testing.T) {
	f := newFakeTreasury(t)
	if _, err := f.run(t, newTreasuryMonitorUpdateCmd(), "", "mon-static", "--alert-enabled=false", "--yes"); err != nil {
		t.Fatal(err)
	}
	put := f.only(t, http.MethodPut)[0]
	if put.Path != "/v3/orgs/org-1/monitoring-configs/mon-static" || string(bytes.TrimSpace(put.Body)) != `{"alertEnabled":false}` {
		t.Fatalf("put = %s %s", put.Path, put.Body)
	}

	out, err := f.run(t, newTreasuryMonitorUpdateCmd(), "", "mon-static", "--approaching", "8", "--breaching", "3", "--notify-on", "APPROACHING", "--yes")
	if err != nil || out != "updated treasury monitor mon-static\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	body := f.only(t, http.MethodPut)[1].JSON(t)
	want := map[string]any{"approaching": map[string]any{"mode": "STATIC", "value": float64(8)}, "breaching": map[string]any{"mode": "STATIC", "value": float64(3)}, "notifyOn": []any{"APPROACHING"}}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %v", body)
	}
}

func TestTreasuryMonitorUpdateValidation(t *testing.T) {
	cases := map[string][]string{
		"nothing to update": {"mon-static"},
		"order":             {"mon-static", "--approaching", "1", "--breaching", "2"},
		"empty notify":      {"mon-static", "--notify-on", ""},
		"input and flags":   {"mon-static", "--input", "-", "--approaching", "3"},
		"single negative":   {"mon-static", "--breaching", "-2"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeTreasury(t)
			if _, err := f.run(t, newTreasuryMonitorUpdateCmd(), "", append(args, "--yes")...); err == nil {
				t.Fatal("expected error")
			}
			f.expectNoRequests(t)
		})
	}
}

func TestTreasuryMonitorUpdateFromInputDryRun(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryMonitorUpdateCmd(), `{"breaching":{"mode":"DYNAMIC","value":0,"rule":{"templateId":"tpl-1","parameters":{},"predicate":"$balance < $total_earned"}}}`,
		"mon-dynamic", "--input", "-", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	f.expectNoRequests(t)
	request := decodeEnvelope(t, out).Request.(map[string]any)
	if request["method"] != "PUT" || request["url"] != f.server.URL+"/v3/orgs/org-1/monitoring-configs/mon-dynamic" {
		t.Fatalf("request = %v", request)
	}
	if _, present := request["body"].(map[string]any)["approaching"]; present {
		t.Fatalf("partial update must omit approaching: %v", request["body"])
	}
}

func TestTreasuryMonitorDelete(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryMonitorDeleteCmd(), "", "mon-static", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	f.expectNoRequests(t)
	if request := decodeEnvelope(t, out).Request.(map[string]any); request["method"] != "DELETE" || request["body"] != nil {
		t.Fatalf("preview = %v", request)
	}
	_, err = f.run(t, newTreasuryMonitorDeleteCmd(), "", "mon-static")
	requireErrorContains(t, err, "without --yes")
	f.expectNoRequests(t)

	out, err = f.run(t, newTreasuryMonitorDeleteCmd(), "", "mon-static", "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	deletes := f.only(t, http.MethodDelete)
	if len(deletes) != 1 || deletes[0].Path != "/v3/orgs/org-1/monitoring-configs/mon-static" || len(deletes[0].Body) != 0 {
		t.Fatalf("deletes = %+v", deletes)
	}
	if envelope := decodeEnvelope(t, out); envelope.Status != "success" || envelope.Result != nil {
		t.Fatalf("envelope = %#v", envelope)
	}
	human, err := f.run(t, newTreasuryMonitorDeleteCmd(), "", "mon-dynamic", "--yes")
	if err != nil || human != "deleted treasury monitor mon-dynamic\n" {
		t.Fatalf("human=%q err=%v", human, err)
	}
}

// ---- monitors: reevaluate --------------------------------------------------

func TestTreasuryMonitorReevaluateResavesThresholdsUnchanged(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryMonitorReevaluateCmd(), "", "mon-dynamic", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	requests := f.recorded()
	if len(requests) != 2 || requests[0].Method != http.MethodGet || requests[1].Method != http.MethodPut || requests[1].Path != "/v3/orgs/org-1/monitoring-configs/mon-dynamic" {
		t.Fatalf("requests = %+v", requests)
	}
	want := map[string]any{
		"approaching": map[string]any{"mode": "STATIC", "value": float64(100)},
		"breaching": map[string]any{"mode": "DYNAMIC", "value": float64(0), "rule": map[string]any{"templateId": "tpl-1",
			"parameters": map[string]any{"party": "validator::1"}, "predicate": "$balance < $total_earned * 0.702"}},
	}
	if got := requests[1].JSON(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("PUT body = %#v\nwant %#v (server-populated rule fields must be stripped)", got, want)
	}
	if out != "re-evaluated mon-dynamic (Canton validator): APPROACHING\n" {
		t.Fatalf("human output = %q", out)
	}
}

func TestTreasuryMonitorReevaluateAllTargetsDynamicMonitors(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryMonitorReevaluateCmd(), "", "--all", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if puts := f.only(t, http.MethodPut); len(puts) != 0 {
		t.Fatalf("dry-run sent PUTs: %+v", puts)
	}
	previews := decodeEnvelope(t, out).Request.([]any)
	if len(previews) != 1 || previews[0].(map[string]any)["url"] != f.server.URL+"/v3/orgs/org-1/monitoring-configs/mon-dynamic" {
		t.Fatalf("previews = %v", previews)
	}

	_, err = f.run(t, newTreasuryMonitorReevaluateCmd(), "", "--all")
	requireErrorContains(t, err, "without --yes")
	if puts := f.only(t, http.MethodPut); len(puts) != 0 {
		t.Fatalf("unconfirmed run sent PUTs: %+v", puts)
	}

	out, err = f.run(t, newTreasuryMonitorReevaluateCmd(), "", "--all", "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if puts := f.only(t, http.MethodPut); len(puts) != 1 {
		t.Fatalf("puts = %+v", puts)
	}
	if result := decodeEnvelope(t, out).Result.(map[string]any); result["count"] != float64(1) {
		t.Fatalf("result = %v", result)
	}
}

func TestTreasuryMonitorReevaluateEdgeCases(t *testing.T) {
	f := newFakeTreasury(t)
	_, err := f.run(t, newTreasuryMonitorReevaluateCmd(), "", "--yes")
	requireErrorContains(t, err, "pass monitor IDs or --all")
	_, err = f.run(t, newTreasuryMonitorReevaluateCmd(), "", "mon-static", "--all", "--yes")
	requireErrorContains(t, err, "pass monitor IDs or --all")
	f.expectNoRequests(t)

	_, err = f.run(t, newTreasuryMonitorReevaluateCmd(), "", "missing", "--yes")
	requireErrorContains(t, err, "get treasury monitor missing")

	f.fail["PUT /v3/orgs/org-1/monitoring-configs/mon-dynamic"] = http.StatusInternalServerError
	_, err = f.run(t, newTreasuryMonitorReevaluateCmd(), "", "mon-static", "mon-dynamic", "--yes")
	requireErrorContains(t, err, "re-evaluate monitor mon-dynamic (2 of 2; 1 already re-evaluated)")

	empty := newFakeTreasury(t)
	delete(empty.monitors, "mon-dynamic")
	empty.order = []string{"mon-static"}
	out, err := empty.run(t, newTreasuryMonitorReevaluateCmd(), "", "--all", "--yes")
	if err != nil || out != "no monitors with dynamic thresholds to re-evaluate\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

// ---- alerts ----------------------------------------------------------------

func TestTreasuryAlertsListFilters(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryAlertsListCmd(), "", "--status", "open", "--tier", "breaching", "--json")
	if err != nil {
		t.Fatal(err)
	}
	request := f.recorded()[0]
	if request.Path != "/v3/orgs/org-1/alerts" || request.RawQuery != "status=OPEN&tier=BREACHING" {
		t.Fatalf("request = %+v", request)
	}
	if object := decodeObject(t, out); object["count"] != float64(1) {
		t.Fatalf("alerts = %v", object)
	}
	table, err := f.run(t, newTreasuryAlertsListCmd(), "")
	if err != nil || !strings.Contains(table, "al-2") || !strings.Contains(table, "RESOLVED") || f.recorded()[1].RawQuery != "" {
		t.Fatalf("table=%q err=%v", table, err)
	}
	for _, args := range [][]string{{"--status", "DONE"}, {"--tier", "HEALTHY"}} {
		g := newFakeTreasury(t)
		_, err := g.run(t, newTreasuryAlertsListCmd(), "", args...)
		requireErrorContains(t, err, "must be one of")
		g.expectNoRequests(t)
	}
}

func TestTreasuryAlertAck(t *testing.T) {
	f := newFakeTreasury(t)
	if _, err := f.run(t, newTreasuryAlertAckCmd(), "", "al-1", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	_, err := f.run(t, newTreasuryAlertAckCmd(), "", "al-1")
	requireErrorContains(t, err, "without --yes")
	f.expectNoRequests(t)

	out, err := f.run(t, newTreasuryAlertAckCmd(), "", "al-1", "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	posts := f.only(t, http.MethodPost)
	if len(posts) != 1 || posts[0].Path != "/v3/orgs/org-1/alerts/al-1/acknowledge" || len(posts[0].Body) != 0 {
		t.Fatalf("posts = %+v", posts)
	}
	if result := decodeEnvelope(t, out).Result.(map[string]any); result["status"] != "ACKNOWLEDGED" {
		t.Fatalf("result = %v", result)
	}
	out, err = f.run(t, newTreasuryAlertAckCmd(), "", "al-2", "--yes", "--json")
	requireErrorContains(t, err, "409")
	if envelope := decodeEnvelope(t, out); envelope.Error == nil || envelope.Error.HTTPStatus != 409 {
		t.Fatalf("envelope = %#v", envelope)
	}
	human, err := f.run(t, newTreasuryAlertAckCmd(), "", "al-1", "--yes")
	if err != nil || human != "acknowledged treasury alert al-1\n" {
		t.Fatalf("human=%q err=%v", human, err)
	}
}

// ---- channels --------------------------------------------------------------

func TestTreasuryChannelsGet(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryChannelsGetCmd(), "", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if channels := decodeObject(t, out)["channels"].([]any); len(channels) != 1 || f.recorded()[0].Path != "/v3/orgs/org-1/alert-channels" {
		t.Fatalf("channels = %v", channels)
	}
	table, err := f.run(t, newTreasuryChannelsGetCmd(), "")
	if err != nil || !strings.Contains(table, "ops@example.com") {
		t.Fatalf("table=%q err=%v", table, err)
	}
	f.channels = []map[string]any{}
	out, err = f.run(t, newTreasuryChannelsGetCmd(), "", "--json")
	if err != nil || !strings.Contains(out, `"channels": []`) {
		t.Fatalf("empty out=%q err=%v", out, err)
	}
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`[]`)) })
	_, err = f.run(t, newTreasuryChannelsGetCmd(), "")
	requireErrorContains(t, err, "unexpected alert-channels response")
}

func TestTreasuryChannelsSet(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryChannelsSetCmd(), "", "--channel", "email", "--target", "ops@example.com, cfo@example.com", "--yes")
	if err != nil || out != "updated 1 treasury alert channel(s)\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	put := f.only(t, http.MethodPut)[0]
	want := map[string]any{"channels": []any{map[string]any{"channel": "EMAIL", "enabled": true, "target": "ops@example.com, cfo@example.com"}}}
	if put.Path != "/v3/orgs/org-1/alert-channels" || !reflect.DeepEqual(put.JSON(t), want) {
		t.Fatalf("put = %s %s", put.Path, put.Body)
	}

	if _, err := f.run(t, newTreasuryChannelsSetCmd(), `{"channels":[{"channel":"SLACK","enabled":true,"target":"#treasury"},{"channel":"EMAIL","enabled":false,"target":""}]}`, "--input", "-", "--yes"); err != nil {
		t.Fatal(err)
	}
	if got := f.only(t, http.MethodPut)[1].JSON(t)["channels"].([]any); len(got) != 2 {
		t.Fatalf("channels = %v", got)
	}
}

func TestTreasuryChannelsSetValidation(t *testing.T) {
	cases := []struct {
		name, stdin string
		args        []string
		want        string
	}{
		{"missing channel", "", []string{"--target", "a@b.co"}, "--channel is required"},
		{"missing target", "", []string{"--channel", "EMAIL"}, "--target is required"},
		{"bad channel", "", []string{"--channel", "SMS", "--target", "x"}, "channel must be one of"},
		{"bad email", "", []string{"--channel", "EMAIL", "--target", "ops@example.com,not-an-email"}, `"not-an-email" is not an email address`},
		{"empty email", "", []string{"--channel", "EMAIL", "--target", ""}, "requires at least one recipient"},
		{"input and flags", "{}", []string{"--input", "-", "--channel", "EMAIL"}, "use either --input or channel flags"},
		{"empty input", `{"channels":[]}`, []string{"--input", "-"}, "channels is required"},
		{"duplicate", `{"channels":[{"channel":"EMAIL","enabled":false,"target":""},{"channel":"email","enabled":false,"target":""}]}`, []string{"--input", "-"}, "listed more than once"},
		{"unknown field", `{"channels":[],"extra":1}`, []string{"--input", "-"}, "unknown field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeTreasury(t)
			_, err := f.run(t, newTreasuryChannelsSetCmd(), tc.stdin, append(tc.args, "--yes")...)
			requireErrorContains(t, err, tc.want)
			f.expectNoRequests(t)
		})
	}
	// Disabling EMAIL may keep an empty draft target, as the backend allows.
	f := newFakeTreasury(t)
	if _, err := f.run(t, newTreasuryChannelsSetCmd(), "", "--channel", "EMAIL", "--target", "", "--enabled=false", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	f.expectNoRequests(t)
}

// ---- analytics templates ---------------------------------------------------

func TestTreasuryTemplatesList(t *testing.T) {
	f := newFakeTreasury(t)
	table, err := f.run(t, newTreasuryTemplatesListCmd(), "")
	if err != nil {
		t.Fatal(err)
	}
	if request := f.recorded()[0]; request.Path != "/v3/orgs/org-1/analytics/templates" || request.RawQuery != "tags=treasury" {
		t.Fatalf("request = %+v", request)
	}
	for _, want := range []string{"tpl-1", "Canton earned", "party*", "$total_earned"} {
		if !strings.Contains(table, want) {
			t.Fatalf("table missing %q:\n%s", want, table)
		}
	}
	if _, err := f.run(t, newTreasuryTemplatesListCmd(), "", "--all", "--json"); err != nil || f.recorded()[1].RawQuery != "" {
		t.Fatalf("--all query = %q err=%v", f.recorded()[1].RawQuery, err)
	}
	out, err := f.run(t, newTreasuryTemplatesListCmd(), "", "--tag", "canton", "--tag", "rwa", "--json")
	if err != nil || f.recorded()[2].RawQuery != "tags=canton%2Crwa" || decodeObject(t, out)["count"] != float64(0) {
		t.Fatalf("tag query = %q out=%s err=%v", f.recorded()[2].RawQuery, out, err)
	}
	_, err = f.run(t, newTreasuryTemplatesListCmd(), "", "--tag", "x", "--all")
	requireErrorContains(t, err, "use either --tag or --all")
}

func TestTreasuryTemplateGet(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryTemplateGetCmd(), "", "tpl-1")
	if err != nil || decodeObject(t, out)["id"] != "tpl-1" || f.recorded()[0].Path != "/v3/orgs/org-1/analytics/templates/tpl-1" {
		t.Fatalf("out=%s err=%v", out, err)
	}
}

func TestTreasuryTemplateExecute(t *testing.T) {
	f := newFakeTreasury(t)
	out, err := f.run(t, newTreasuryTemplateExecuteCmd(), "", "tpl-1", "--param", "party=validator::1", "--cost-tier", "MEDIUM", "--confirm-large-query", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	f.expectNoRequests(t)
	body := decodeEnvelope(t, out).Request.(map[string]any)["body"].(map[string]any)
	want := map[string]any{"templateId": "tpl-1", "parameters": map[string]any{"party": "validator::1"}, "costTier": "medium", "confirmLargeQuery": true}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %v", body)
	}
	_, err = f.run(t, newTreasuryTemplateExecuteCmd(), "", "tpl-1")
	requireErrorContains(t, err, "refusing to run a billed analytics query without --yes")
	_, err = f.run(t, newTreasuryTemplateExecuteCmd(), "", "tpl-1", "--cost-tier", "huge", "--yes")
	requireErrorContains(t, err, "--cost-tier must be one of")
	_, err = f.run(t, newTreasuryTemplateExecuteCmd(), "", "tpl-1", "--param", "novalue", "--yes")
	requireErrorContains(t, err, "must be key=value")
	f.expectNoRequests(t)

	human, err := f.run(t, newTreasuryTemplateExecuteCmd(), "", "tpl-1", "--param", "party=validator::1", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	post := f.only(t, http.MethodPost)[0]
	if post.Path != "/v3/orgs/org-1/analytics/queries/execute" || !reflect.DeepEqual(post.JSON(t), map[string]any{"templateId": "tpl-1", "parameters": map[string]any{"party": "validator::1"}}) {
		t.Fatalf("post = %s %s", post.Path, post.Body)
	}
	if !strings.Contains(human, "execution exec-1: completed") || !strings.Contains(human, "total_earned") || !strings.Contains(human, "1234.5") {
		t.Fatalf("human = %q", human)
	}
}

// ---- helpers and registration ----------------------------------------------

func TestTreasuryParamValueKeepsJSONLiteralTypes(t *testing.T) {
	cases := map[string]any{"10": json.Number("10"), "true": true, `"10"`: "10", "abc": "abc", "": "", "1 2": "1 2", "null": nil, "validator::1": "validator::1"}
	for raw, want := range cases {
		if got := treasuryParamValue(raw); !reflect.DeepEqual(got, want) {
			t.Fatalf("treasuryParamValue(%q) = %#v, want %#v", raw, got, want)
		}
	}
}

func TestNormalizeTreasuryTierLegacyResultColumnAndNonFinite(t *testing.T) {
	tier := &treasuryThreshold{Mode: "dynamic", Rule: &treasuryRule{ResultColumn: "floor"}}
	if err := normalizeTreasuryTier("breaching", tier); err != nil || tier.Mode != treasuryModeDynamic || tier.Value == nil || tier.Rule.Parameters == nil {
		t.Fatalf("tier = %#v err=%v", tier, err)
	}
	nan := 0.0
	nan = nan / nan
	for _, candidate := range []*treasuryThreshold{{Value: &nan}, {Mode: "DYNAMIC", Value: &nan, Rule: &treasuryRule{Predicate: "$balance < 1"}}} {
		requireErrorContains(t, normalizeTreasuryTier("approaching", candidate), "must be a number")
	}
}

func TestTreasuryPresentationHelpers(t *testing.T) {
	if got := describeTreasuryTier(treasuryThreshold{Mode: "DYNAMIC", Rule: &treasuryRule{ResultColumn: "floor"}}); got != "DYNAMIC($balance <= $floor)" {
		t.Fatalf("describe = %q", got)
	}
	if got := describeTreasuryTier(treasuryThreshold{Mode: "STATIC"}); got != "-" {
		t.Fatalf("describe = %q", got)
	}
	if got := formatTreasuryQueryResult(json.RawMessage(`{"execution":{"id":"e","status":"failed"}}`)); got != "execution e: failed\n" {
		t.Fatalf("format = %q", got)
	}
	if got := formatTreasuryQueryResult(json.RawMessage(`"odd"`)); got != "\"odd\"\n" {
		t.Fatalf("format = %q", got)
	}
	if got := treasuryPath("org/1", "monitoring-configs", "a b"); got != "/v3/orgs/org%2F1/monitoring-configs/a%20b" {
		t.Fatalf("path = %q", got)
	}
}

func TestTreasuryOrganizationScopeIsEnforced(t *testing.T) {
	f := newFakeTreasury(t)
	for _, definition := range []*op.Definition{newTreasuryMonitorsListCmd(), newTreasuryMonitorDeleteCmd()} {
		args := []string{"--org", "other-org"}
		if definition.Args != nil && definition.Args.Min == 1 {
			args = append(args, "mon-static", "--yes")
		}
		_, err := f.run(t, definition, "", args...)
		requireErrorContains(t, err, "conflicts with the invocation scope")
	}
	f.expectNoRequests(t)
}

func TestTreasuryCommandTreeIsRegistered(t *testing.T) {
	descriptors, err := op.Descriptors(NewRoot())
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, descriptor := range descriptors {
		names[descriptor.Name] = true
	}
	for _, want := range []string{
		"bitwave_treasury_monitors_list", "bitwave_treasury_monitors_get", "bitwave_treasury_monitors_create", "bitwave_treasury_monitors_update",
		"bitwave_treasury_monitors_delete", "bitwave_treasury_monitors_history", "bitwave_treasury_monitors_reevaluate",
		"bitwave_treasury_alerts_list", "bitwave_treasury_alerts_ack", "bitwave_treasury_channels_get", "bitwave_treasury_channels_set",
		"bitwave_treasury_templates_list", "bitwave_treasury_templates_get", "bitwave_treasury_templates_execute",
	} {
		if !names[want] {
			t.Errorf("operation %s is not registered", want)
		}
	}
	// The terminal adapter resolves the same tree through Cobra.
	root := NewRootCmd()
	if command, _, err := root.Find([]string{"treasury", "monitor", "reevaluate"}); err != nil || command.Name() != "reevaluate" {
		t.Fatalf("alias lookup = %v, %v", command, err)
	}
}
