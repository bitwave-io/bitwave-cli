package shares

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClient_Create_PostsJSON(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_, _ = w.Write([]byte(`{
			"shareId":"sh-1","orgId":"org-1","journalId":"j-1","token":"tok",
			"recipientEmail":"a@b.co","status":"ACTIVE","expiresAt":"2099-01-01T00:00:00Z",
			"viewCount":0,"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z",
			"url":"https://example.com/public/shares/tok"
		}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "org-1", "ws-1", func() (string, error) { return "tok-abc", nil })
	out, err := c.Create(context.Background(), "j-1", CreateRequest{
		RecipientEmail: "a@b.co",
		Message:        "hi",
		TTLHours:       24,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method: %s", gotMethod)
	}
	if gotPath != "/v1/orgs/org-1/workspaces/ws-1/journals/j-1/shares" {
		t.Errorf("path: %s", gotPath)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Errorf("auth: %s", gotAuth)
	}
	if gotBody["recipientEmail"] != "a@b.co" {
		t.Errorf("body recipientEmail: %v", gotBody["recipientEmail"])
	}
	if _, hasSnapshot := gotBody["snapshot"]; hasSnapshot {
		t.Errorf("body unexpectedly carries snapshot field: %v", gotBody)
	}
	if out.URL != "https://example.com/public/shares/tok" {
		t.Errorf("URL: %s", out.URL)
	}
	if out.ShareId != "sh-1" {
		t.Errorf("shareId: %s", out.ShareId)
	}
}

func TestClient_Create_PropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad email"}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "org-1", "ws-1", func() (string, error) { return "t", nil })
	_, err := c.Create(context.Background(), "j-1", CreateRequest{RecipientEmail: "nope"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected 400 in error, got: %v", err)
	}
}

func TestClient_List(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`[
			{"shareId":"a","orgId":"o","journalId":"j","token":"t1","recipientEmail":"x@y.z","status":"ACTIVE","expiresAt":"2099-01-01T00:00:00Z"},
			{"shareId":"b","orgId":"o","journalId":"j","token":"t2","recipientEmail":"x@y.z","status":"REVOKED","expiresAt":"2099-01-01T00:00:00Z"}
		]`))
	}))
	defer srv.Close()
	c := New(srv.URL, "o", "ws-1", func() (string, error) { return "t", nil })
	out, err := c.List(context.Background(), "j")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/v1/orgs/o/workspaces/ws-1/journals/j/shares" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	if len(out) != 2 || out[0].ShareId != "a" {
		t.Errorf("list result: %+v", out)
	}
}

func TestClient_Revoke(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"shareId":"sh-1","status":"REVOKED","expiresAt":"2099-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "o", "ws-1", func() (string, error) { return "t", nil })
	out, err := c.Revoke(context.Background(), "j-1", "sh-1")
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if path != "/v1/orgs/o/workspaces/ws-1/journals/j-1/shares/sh-1/revoke" {
		t.Errorf("path: %s", path)
	}
	if out.Status != "REVOKED" {
		t.Errorf("status: %s", out.Status)
	}
}

func TestClient_WithoutOrgOrWorkspace_RefusesBeforeAnyRequest(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	for _, ids := range [][2]string{{"", "ws-1"}, {"org-1", ""}} {
		c := New(srv.URL, ids[0], ids[1], func() (string, error) { return "t", nil })
		if _, err := c.Create(context.Background(), "j-1", CreateRequest{RecipientEmail: "a@b.co"}); err == nil {
			t.Errorf("create with org=%q workspace=%q: expected error", ids[0], ids[1])
		}
		if _, err := c.List(context.Background(), "j-1"); err == nil {
			t.Errorf("list with org=%q workspace=%q: expected error", ids[0], ids[1])
		}
		if _, err := c.Revoke(context.Background(), "j-1", "sh-1"); err == nil {
			t.Errorf("revoke with org=%q workspace=%q: expected error", ids[0], ids[1])
		}
	}
	if called {
		t.Fatal("a share request without an org or workspace reached the server")
	}
}

var _ = time.Time{}
