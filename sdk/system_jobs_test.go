package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSystemJobCancellationUsesScopedGraphQLMutation(t *testing.T) {
	calls := 0
	options := Options{OrganizationID: "session-org", Token: "test-token", HTTPClient: &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body.Query, "cancelSystemJob(orgId: $orgId, jobRunId: $jobRunId)") || body.Variables["orgId"] != "session-org" || body.Variables["jobRunId"] != "run-1" {
			t.Fatalf("wrong request: %+v", body)
		}
		return contractResponse(r, `{"data":{"cancelSystemJob":{"jobRunId":"run-1","status":"cancelled"}}}`), nil
	})}}
	client := NewClient(options)
	// JSON input cannot replace the authenticated org or positional job identifier.
	_, err := client.Invoke(context.Background(), Request{Operation: "bitwave_org_admin_system_jobs_cancel", Arguments: json.RawMessage(`{"arguments":["run-1"],"data":"{\"orgId\":\"other\",\"jobRunId\":\"other-run\"}","yes":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestSystemJobCancellationRequiresJobAndConfirmation(t *testing.T) {
	for _, input := range []string{`{"yes":true}`, `{"arguments":["run-1"]}`} {
		calls := 0
		client := NewClient(Options{OrganizationID: "org-1", Token: "test-token", HTTPClient: &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) { calls++; return contractResponse(r, `{}`), nil })}})
		_, err := client.Invoke(context.Background(), Request{Operation: "bitwave_org_admin_system_jobs_cancel", Arguments: json.RawMessage(input)})
		if err == nil || calls != 0 {
			t.Fatalf("input=%s error=%v calls=%d", input, err, calls)
		}
	}
}

func cancelEnvelope(t *testing.T, responseBody string) map[string]any {
	t.Helper()
	client := NewClient(Options{OrganizationID: "org-1", Token: "test-token", HTTPClient: &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
		return contractResponse(r, responseBody), nil
	})}})
	result, err := client.Invoke(context.Background(), Request{Operation: "bitwave_org_admin_system_jobs_cancel", Arguments: json.RawMessage(`{"arguments":["run-1"],"yes":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(result.Data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCancellationStatusIsLiftedToTheEnvelopeSoPendingIsNotMistakenForCancelled(t *testing.T) {
	cases := map[string]string{
		"cancelled":             "cloud confirmed",
		"cancellationRequested": "not confirmed yet",
		"alreadyFinished":       "nothing was cancelled",
	}
	for status, noteFragment := range cases {
		out := cancelEnvelope(t, `{"data":{"cancelSystemJob":{"jobRunId":"run-1","status":"`+status+`"}}}`)
		if out["status"] != "success" {
			t.Fatalf("%s: the transport status is still success: %v", status, out)
		}
		if out["cancellationStatus"] != status {
			t.Fatalf("%s: cancellationStatus = %v", status, out["cancellationStatus"])
		}
		if note, _ := out["note"].(string); !strings.Contains(note, noteFragment) {
			t.Fatalf("%s: note = %q", status, note)
		}
	}
}

func TestUnrecognizedCancellationStatusIsNotLifted(t *testing.T) {
	out := cancelEnvelope(t, `{"data":{"cancelSystemJob":{"jobRunId":"run-1","status":"somethingNew"}}}`)
	if _, present := out["cancellationStatus"]; present {
		t.Fatalf("an unknown status must be left in result for the caller, not asserted: %v", out)
	}
}
