package orgreports

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBalanceReportLifecycle(t *testing.T) {
	var statusCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v2/orgs/org-1/report-runs":
			if r.Method != http.MethodPost {
				t.Fatalf("start method = %s", r.Method)
			}
			var body StartRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.ReportType != BalanceReportType || len(body.Inputs) == 0 {
				t.Fatalf("unexpected start request: %#v", body)
			}
			_, _ = w.Write([]byte(`{"successfullyStarted":true,"reportRunId":"run-1"}`))
		case "/v2/orgs/org-1/report-runs/run-1/status":
			statusCalls++
			_, _ = w.Write([]byte(`{"status":"succeeded"}`))
		case "/v2/orgs/org-1/report-runs/run-1/download":
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte("Wallet,Asset,Balance\nTreasury,BTC,1\n"))
		case "/v2/orgs/org-1/report-runs/run-1":
			_, _ = w.Write([]byte(`{"reportType":"balance-report","reportRunId":"run-1","columns":["Wallet","Asset","Balance"],"rows":[{"cells":["Treasury","BTC","1"],"rows":[]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, func() (string, error) { return "token", nil })
	run, err := c.StartBalance(context.Background(), "org-1", []Input{{Key: "endDate", Value: "2026-06-30"}})
	if err != nil || run.ReportRunID != "run-1" {
		t.Fatalf("start: run=%#v err=%v", run, err)
	}
	status, err := c.Status(context.Background(), "org-1", run.ReportRunID)
	if err != nil || status.Status != "succeeded" || statusCalls != 1 {
		t.Fatalf("status: %#v calls=%d err=%v", status, statusCalls, err)
	}
	csv, err := c.Download(context.Background(), "org-1", run.ReportRunID)
	if err != nil || string(csv) != "Wallet,Asset,Balance\nTreasury,BTC,1\n" {
		t.Fatalf("download: %q err=%v", csv, err)
	}
	result, err := c.Result(context.Background(), "org-1", run.ReportRunID)
	if err != nil || len(result.Rows) != 1 || result.Rows[0].Cells[0] != "Treasury" {
		t.Fatalf("result: %#v err=%v", result, err)
	}
}

func TestStartBalanceRequiresRunID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"successfullyStarted":false,"error":"not enabled"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, func() (string, error) { return "token", nil })
	if _, err := c.StartBalance(context.Background(), "org-1", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestLegacyBalanceLifecycleAndExternalDownloadDoesNotLeakToken(t *testing.T) {
	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("authorization leaked to signed download host: %q", got)
		}
		_, _ = w.Write([]byte("Ticker,Amount\nBTC,1\n"))
	}))
	defer download.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/reports/view":
			if r.URL.Query().Get("type") != "BalanceReport" || r.URL.Query().Get("groupBy") != "wallet" {
				t.Fatalf("query = %v", r.URL.Query())
			}
			_, _ = w.Write([]byte(`{"id":"legacy-1"}`))
		case "/v2/orgs/org-1/reports/legacy-1":
			_, _ = w.Write([]byte(`{"data":{"id":"legacy-1","status":"succeeded"},"links":{"results":{"href":"` + download.URL + `","method":"get"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	c := New(api.URL, func() (string, error) { return "token", nil })
	run, err := c.StartLegacyBalance(context.Background(), "org-1", LegacyBalanceInput{EndDate: "2026-06-30", GroupBy: "wallet"})
	if err != nil || run.ID != "legacy-1" {
		t.Fatalf("start: %#v err=%v", run, err)
	}
	report, err := c.LegacyReport(context.Background(), "org-1", run.ID, true)
	if err != nil || report.Data.Status != "succeeded" {
		t.Fatalf("report: %#v err=%v", report, err)
	}
	data, err := c.DownloadLink(context.Background(), report.Links["results"].Href)
	if err != nil || string(data) != "Ticker,Amount\nBTC,1\n" {
		t.Fatalf("download: %q err=%v", data, err)
	}
}
