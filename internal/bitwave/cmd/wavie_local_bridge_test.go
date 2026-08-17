package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateLoopbackAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1:7314", "localhost:7314", "[::1]:7314"} {
		if err := validateLoopbackAddress(address); err != nil {
			t.Fatalf("%s rejected: %v", address, err)
		}
	}
	if err := validateLoopbackAddress("0.0.0.0:7314"); err == nil {
		t.Fatal("expected non-loopback address to be rejected")
	}
}

func TestWavieBridgeStatusAndCORS(t *testing.T) {
	bridge := newWavieBridge("/bin/echo", t.TempDir(), []string{"https://app3.bitwave.io"})
	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	req.Header.Set("Origin", "https://app3.bitwave.io")
	response := httptest.NewRecorder()
	bridge.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "https://app3.bitwave.io" {
		t.Fatalf("allow origin = %q", response.Header().Get("Access-Control-Allow-Origin"))
	}
	var status wavieBridgeStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if !status.Connected || status.Tool.Name != wavieLocalToolName || status.Tool.Safety != "write" {
		t.Fatalf("status = %#v", status)
	}
	if !strings.Contains(status.Tool.Description, "ordinary user intent") || !strings.Contains(status.Tool.Description, "never require the user to mention the CLI") {
		t.Fatalf("tool description does not require intent-based routing: %q", status.Tool.Description)
	}

	blocked := httptest.NewRecorder()
	blockedRequest := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	blockedRequest.Header.Set("Origin", "https://malicious.example")
	bridge.ServeHTTP(blocked, blockedRequest)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("blocked status = %d", blocked.Code)
	}
}

func TestClassifyBitwaveArgs(t *testing.T) {
	for _, args := range [][]string{
		{"--help"}, {"--quiet", "report", "balance", "--as-of", "2024-12-31"},
		{"bal", "--to", "2024-12-31"}, {"org", "current"},
		{"transactions", "search", "--to", "2024-12-31"}, {"rules", "list"},
	} {
		if risk := classifyBitwaveArgs(args); risk != "read" {
			t.Fatalf("classifyBitwaveArgs(%q) = %q, want read", args, risk)
		}
	}
	for _, args := range [][]string{{"org", "use", "abc"}, {"transactions", "categorize", "txn"}, {"rules", "create"}} {
		if risk := classifyBitwaveArgs(args); risk != "write" {
			t.Fatalf("classifyBitwaveArgs(%q) = %q, want write", args, risk)
		}
	}
	for _, args := range [][]string{{"migrate"}, {"wallets", "send"}} {
		if risk := classifyBitwaveArgs(args); risk != "destructive" {
			t.Fatalf("classifyBitwaveArgs(%q) = %q, want destructive", args, risk)
		}
	}
}

func TestWavieBridgeExecutesApprovedCommandAndCachesResult(t *testing.T) {
	bridge := newWavieBridge("/bin/echo", t.TempDir(), nil)
	body := `{"requestId":"tool-1","args":["hello","world"],"reason":"read version","approved":true}`
	execute := func() localCommandResult {
		req := httptest.NewRequest(http.MethodPost, "/v1/execute", strings.NewReader(body))
		req.Header.Set(wavieBridgeHeader, wavieBridgeProtocol)
		response := httptest.NewRecorder()
		bridge.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
		}
		var result localCommandResult
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := execute()
	second := execute()
	if first.ExitCode != 0 || first.Stdout != "hello world\n" {
		t.Fatalf("first = %#v", first)
	}
	if !bytes.Equal(mustJSON(t, first), mustJSON(t, second)) {
		t.Fatalf("cached result differs: first=%#v second=%#v", first, second)
	}
	if len(bridge.results) != 1 {
		t.Fatalf("cached results = %d", len(bridge.results))
	}
}

func TestWavieBridgeBindsExecutionToSessionOrganization(t *testing.T) {
	bridge := newWavieBridge("/usr/bin/env", t.TempDir(), nil)
	body := `{"requestId":"tool-org","organizationId":"org-from-session","args":[],"reason":"verify org scope","approved":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/execute", strings.NewReader(body))
	req.Header.Set(wavieBridgeHeader, wavieBridgeProtocol)
	response := httptest.NewRecorder()
	bridge.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var result localCommandResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Stdout, "BITWAVE_ORG_ID=org-from-session") {
		t.Fatalf("session organization missing from environment: %s", result.Stdout)
	}
}

func TestWavieBridgeAutoRunsReadsAndRequiresApprovalForWrites(t *testing.T) {
	bridge := newWavieBridge("/path/that/does/not/exist", t.TempDir(), nil)
	for name, testCase := range map[string]struct {
		body     string
		header   string
		expected int
	}{
		"read without approval executes": {`{"requestId":"tool-1","args":["--version"],"reason":"test","approved":false}`, wavieBridgeProtocol, http.StatusOK},
		"write without approval":         {`{"requestId":"tool-3","args":["org","use","abc"],"reason":"test","approved":false}`, wavieBridgeProtocol, http.StatusPreconditionRequired},
		"no header":                      {`{"requestId":"tool-2","args":["--version"],"reason":"test","approved":true}`, "", http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/execute", strings.NewReader(testCase.body))
			if testCase.header != "" {
				req.Header.Set(wavieBridgeHeader, testCase.header)
			}
			response := httptest.NewRecorder()
			bridge.ServeHTTP(response, req)
			if response.Code != testCase.expected {
				t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
