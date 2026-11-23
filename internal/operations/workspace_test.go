package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bitwave-io/bitwave-cli/internal/bitwave/config"
)

func TestWorkspaceCreate_PostsInsideActiveOrg(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BITWAVE_ORG_ID", "org-1")
	t.Setenv("BITWAVE_TOKEN", "tok")
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"id":"ws-new"}`))
	}))
	defer srv.Close()
	t.Setenv("BITWAVE_BASE_URL_GL", srv.URL)

	cmd := testDefinition(newWorkspaceCmd())
	cmd.SetArgs([]string{"create", "--name", "Books"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v\n%s", err, out.String())
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/orgs/org-1/workspaces" {
		t.Errorf("request: %s %s", gotMethod, gotPath)
	}
	if gotBody["name"] != "Books" || gotBody["baseCurrency"] != "USD" {
		t.Errorf("body: %v", gotBody)
	}
	if !strings.Contains(out.String(), "ws-new") {
		t.Errorf("output: %s", out.String())
	}
}

func TestLedgerCommand_CloudWorkspaceWithoutOrg_FailsWithFix(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BITWAVE_ORG_ID", "org-1")
	t.Setenv("BITWAVE_TOKEN", "tok")
	dir := setupWorkspace(t)
	if err := config.Save(dir, &config.Config{Mode: config.ModeCloud, WorkspaceId: "ws-legacy"}); err != nil {
		t.Fatal(err)
	}
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	t.Setenv("BITWAVE_BASE_URL_GL", srv.URL)

	cmd := testDefinition(newJournalCmd())
	cmd.SetArgs([]string{"list"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected an error for a cloud workspace without org_id")
	}
	for _, want := range []string{"org_id", "ws-legacy", "bitwave org use", "bitwave workspace use ws-legacy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if called {
		t.Fatal("an org-less workspace reached the ledger")
	}
}

func TestWorkspaceUse_RecordsOrgOnOrgLessCloudWorkspace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BITWAVE_ORG_ID", "org-1")
	t.Setenv("BITWAVE_TOKEN", "tok")
	dir := setupWorkspace(t)
	if err := config.Save(dir, &config.Config{Mode: config.ModeCloud, WorkspaceId: "ws-legacy", Name: "Legacy"}); err != nil {
		t.Fatal(err)
	}
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/v1/orgs/org-1/workspaces":
			_, _ = w.Write([]byte(`[{"id":"ws-legacy","orgId":"org-1","name":"Legacy","baseCurrency":"USD"}]`))
		case "/v1/orgs/org-1/workspaces/ws-legacy/ledger/journals":
			_, _ = w.Write([]byte(`[{"id":"default","name":"Default"}]`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("BITWAVE_BASE_URL_GL", srv.URL)

	for _, args := range [][]string{{"workspace", "use", "ws-legacy"}, {"journal", "list"}} {
		cmd := NewRootCmd()
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
	}
	cfg, err := config.Load(dir)
	if err != nil || cfg.OrgId != "org-1" || cfg.WorkspaceId != "ws-legacy" {
		t.Fatalf("rebind did not record the org: %+v %v", cfg, err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %v", requests)
	}
}
