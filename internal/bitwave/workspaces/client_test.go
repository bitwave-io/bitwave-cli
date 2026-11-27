package workspaces

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateWorkspace_PassesNotifyEmail_AndParsesURL(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_, _ = w.Write([]byte(`{"id":"ws-1","url":"https://api.bitwave.io/ui/workspaces/ws-1"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "org-1", func() (string, error) { return "tok-abc", nil })
	res, err := c.CreateWorkspace(CreateWorkspaceRequest{
		Name:         "acme",
		BaseCurrency: "USD",
		NotifyEmail:  "pat@bitwave.io",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method: %s", gotMethod)
	}
	if gotPath != "/v1/orgs/org-1/workspaces" {
		t.Errorf("path: %s", gotPath)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Errorf("auth: %s", gotAuth)
	}
	if gotBody["notifyEmail"] != "pat@bitwave.io" {
		t.Errorf("body notifyEmail: %v", gotBody["notifyEmail"])
	}
	if _, present := gotBody["orgId"]; present {
		t.Errorf("org belongs in the path, not the body: %v", gotBody)
	}
	if res.Id != "ws-1" {
		t.Errorf("id: %s", res.Id)
	}
	if res.URL != "https://api.bitwave.io/ui/workspaces/ws-1" {
		t.Errorf("url: %s", res.URL)
	}
}

func TestCreateWorkspace_OmitsNotifyEmailWhenEmpty(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_, _ = w.Write([]byte(`{"id":"ws-1","url":"https://x/ui/workspaces/ws-1"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "org-1", func() (string, error) { return "t", nil })
	if _, err := c.CreateWorkspace(CreateWorkspaceRequest{Name: "a", BaseCurrency: "USD"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, present := gotBody["notifyEmail"]; present {
		t.Errorf("notifyEmail should be omitted when empty, got: %v", gotBody)
	}
}

func TestCreateWorkspace_AbsentURL_OlderServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Older server: no url field at all.
		_, _ = w.Write([]byte(`{"id":"ws-1"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "org-1", func() (string, error) { return "t", nil })
	res, err := c.CreateWorkspace(CreateWorkspaceRequest{Name: "a", BaseCurrency: "USD"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.Id != "ws-1" {
		t.Errorf("id: %s", res.Id)
	}
	if res.URL != "" {
		t.Errorf("url should be empty on older server, got: %q", res.URL)
	}
}

func TestCreateWorkspace_PropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad name"}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "org-1", func() (string, error) { return "t", nil })
	_, err := c.CreateWorkspace(CreateWorkspaceRequest{Name: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected 400 in error, got: %v", err)
	}
}

func TestGetWorkspace(t *testing.T) {
	tests := []struct {
		name      string
		respBody  string
		wantURL   string
		wantError bool
	}{
		{
			name:     "with url",
			respBody: `{"id":"ws-1","orgId":"org-1","name":"acme","baseCurrency":"USD","url":"https://x/ui/workspaces/ws-1"}`,
			wantURL:  "https://x/ui/workspaces/ws-1",
		},
		{
			name:     "older server without url",
			respBody: `{"id":"ws-1","orgId":"org-1","name":"acme","baseCurrency":"USD"}`,
			wantURL:  "",
		},
		{
			name:      "empty id is a parse error",
			respBody:  `{"name":"acme"}`,
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, _ = w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			c := New(srv.URL, "org-1", func() (string, error) { return "t", nil })
			w, err := c.GetWorkspace("ws-1")
			if tt.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if gotPath != "/v1/orgs/org-1/workspaces/ws-1" {
				t.Errorf("path: %s", gotPath)
			}
			if w.URL != tt.wantURL {
				t.Errorf("url: got %q want %q", w.URL, tt.wantURL)
			}
		})
	}
}

func TestClient_AddressesEveryWorkspaceInsideItsOrg(t *testing.T) {
	tests := []struct {
		name       string
		call       func(c *Client) error
		wantMethod string
		wantPath   string
		respBody   string
	}{
		{
			name:       "list workspaces",
			call:       func(c *Client) error { _, err := c.ListWorkspaces(); return err },
			wantMethod: http.MethodGet,
			wantPath:   "/v1/orgs/org-1/workspaces",
			respBody:   `[]`,
		},
		{
			name: "create workspace",
			call: func(c *Client) error {
				_, err := c.CreateWorkspace(CreateWorkspaceRequest{Name: "a", BaseCurrency: "USD"})
				return err
			},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/orgs/org-1/workspaces",
			respBody:   `{"id":"ws-1"}`,
		},
		{
			name:       "get workspace",
			call:       func(c *Client) error { _, err := c.GetWorkspace("ws-1"); return err },
			wantMethod: http.MethodGet,
			wantPath:   "/v1/orgs/org-1/workspaces/ws-1",
			respBody:   `{"id":"ws-1"}`,
		},
		{
			name:       "list journals",
			call:       func(c *Client) error { _, err := c.ListJournals("ws-1"); return err },
			wantMethod: http.MethodGet,
			wantPath:   "/v1/orgs/org-1/workspaces/ws-1/ledger/journals",
			respBody:   `[]`,
		},
		{
			name: "create journal",
			call: func(c *Client) error {
				_, err := c.CreateJournal("ws-1", CreateJournalRequest{Id: "j-1", Name: "J"})
				return err
			},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/orgs/org-1/workspaces/ws-1/ledger/journals",
			respBody:   `{"id":"j-1"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod, gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				_, _ = w.Write([]byte(tt.respBody))
			}))
			defer srv.Close()

			c := New(srv.URL, "org-1", func() (string, error) { return "t", nil })
			if err := tt.call(c); err != nil {
				t.Fatalf("call: %v", err)
			}
			if gotMethod != tt.wantMethod || gotPath != tt.wantPath {
				t.Errorf("request: got %s %s, want %s %s", gotMethod, gotPath, tt.wantMethod, tt.wantPath)
			}
		})
	}
}

func TestClient_WithoutOrg_RefusesBeforeAnyRequest(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()

	c := New(srv.URL, "", func() (string, error) { return "t", nil })
	calls := map[string]func() error{
		"list workspaces":  func() error { _, err := c.ListWorkspaces(); return err },
		"create workspace": func() error { _, err := c.CreateWorkspace(CreateWorkspaceRequest{Name: "a"}); return err },
		"get workspace":    func() error { _, err := c.GetWorkspace("ws-1"); return err },
		"list journals":    func() error { _, err := c.ListJournals("ws-1"); return err },
		"create journal":   func() error { _, err := c.CreateJournal("ws-1", CreateJournalRequest{Name: "J"}); return err },
	}
	for name, call := range calls {
		if err := call(); err == nil || !strings.Contains(err.Error(), "org") {
			t.Errorf("%s: expected a missing-org error, got %v", name, err)
		}
	}
	if called {
		t.Fatal("a workspace request without an org reached the server")
	}
}
