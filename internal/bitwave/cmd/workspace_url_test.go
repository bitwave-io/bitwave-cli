package cmd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/bitwave-io/bitwave-cli/internal/bitwave/config"
	"github.com/bitwave-io/bitwave-cli/internal/orgctx"
)

// captureStdout runs fn while os.Stdout is redirected, returning what was
// printed. The CLI commands print user-facing output via fmt.Println to the
// real stdout (repo convention), so cmd.SetOut does not capture it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	fn()
	_ = w.Close()
	data, _ := io.ReadAll(r)
	return string(data)
}

// setActiveOrg points ~/.bitwave at a temp HOME and records an active org so
// requireActiveOrg succeeds inside tests. A BITWAVE_TOKEN is set so the org
// token resolver returns without touching the credentials file.
func setActiveOrg(t *testing.T, orgID string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BITWAVE_TOKEN", "test-token")
	if err := orgctx.Save(&orgctx.Active{OrgID: orgID}); err != nil {
		t.Fatalf("save active org: %v", err)
	}
}

func TestWorkspaceURL_CloudMode_PrintsURL(t *testing.T) {
	dir := setupWorkspace(t)
	setActiveOrg(t, "org-1")

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Mode = config.ModeCloud
	cfg.OrgId = "org-1"
	cfg.WorkspaceId = "ws-1"
	if err := config.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"id":"ws-1","orgId":"org-1","name":"acme","baseCurrency":"USD","url":"https://api.bitwave.io/ui/workspaces/ws-1"}`))
	}))
	defer srv.Close()
	t.Setenv("BITWAVE_BASE_URL_GL", srv.URL)

	cmd := newWorkspaceURLCmd()
	cmd.SetArgs([]string{})
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	var execErr error
	out := captureStdout(t, func() {
		execErr = cmd.ExecuteContext(context.Background())
	})
	if execErr != nil {
		t.Fatalf("execute: %v\n%s", execErr, errBuf.String())
	}
	if gotPath != "/v1/workspaces/ws-1" {
		t.Errorf("path: %s", gotPath)
	}
	if got := strings.TrimSpace(out); got != "https://api.bitwave.io/ui/workspaces/ws-1" {
		t.Errorf("output: %q", got)
	}
}

func TestWorkspaceURL_LocalMode_Errors(t *testing.T) {
	// setupWorkspace leaves the workspace in local mode.
	setupWorkspace(t)

	cmd := newWorkspaceURLCmd()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error in local mode")
	}
	if !strings.Contains(err.Error(), "not cloud") {
		t.Errorf("expected local-mode error, got: %v", err)
	}
}

func TestWorkspaceURL_ServerWithoutURL_Errors(t *testing.T) {
	dir := setupWorkspace(t)
	setActiveOrg(t, "org-1")

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Mode = config.ModeCloud
	cfg.OrgId = "org-1"
	cfg.WorkspaceId = "ws-1"
	if err := config.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Older server: no url field.
		_, _ = w.Write([]byte(`{"id":"ws-1","orgId":"org-1","name":"acme","baseCurrency":"USD"}`))
	}))
	defer srv.Close()
	t.Setenv("BITWAVE_BASE_URL_GL", srv.URL)

	cmd := newWorkspaceURLCmd()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err = cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error when server has no url")
	}
	if !strings.Contains(err.Error(), "no URL") {
		t.Errorf("expected no-URL error, got: %v", err)
	}
}
