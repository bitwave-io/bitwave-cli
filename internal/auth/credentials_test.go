package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func useTemporaryCredentials(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func tokenServer(t *testing.T, calls *atomic.Int32, refreshToken string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse token request: %v", err)
		}
		if got := r.Form.Get("client_id"); got != "" {
			t.Errorf("client_id = %q, want omitted for Bitwave refresh grant", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-new", "id_token": "id-new",
			"refresh_token": refreshToken, "expires_in": 3600,
		})
	}))
}

func TestLoadAndRefreshWithOrgReusesCachedOrgToken(t *testing.T) {
	useTemporaryCredentials(t)
	var calls atomic.Int32
	server := tokenServer(t, &calls, "refresh-new")
	defer server.Close()

	if err := SaveCredentials(&Credentials{
		AccessToken: "access-cached", RefreshToken: "refresh-old",
		ExpiresAt: time.Now().Add(time.Hour).Unix(), OrgID: "org-1",
	}); err != nil {
		t.Fatal(err)
	}

	token, err := LoadAndRefreshWithOrg(server.URL, "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if token != "access-cached" || calls.Load() != 0 {
		t.Fatalf("token = %q, token endpoint calls = %d", token, calls.Load())
	}
}

func TestLoadAndRefreshWithOrgExchangesOnceAndCaches(t *testing.T) {
	useTemporaryCredentials(t)
	var calls atomic.Int32
	server := tokenServer(t, &calls, "refresh-new")
	defer server.Close()

	if err := SaveCredentials(&Credentials{
		AccessToken: "access-base", RefreshToken: "refresh-old",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		token, err := LoadAndRefreshWithOrg(server.URL, "org-1")
		if err != nil {
			t.Fatal(err)
		}
		if token != "access-new" {
			t.Fatalf("token = %q, want access-new", token)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", calls.Load())
	}
	creds, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if creds.OrgID != "org-1" || creds.RefreshToken != "refresh-new" {
		t.Fatalf("saved credentials = %+v", creds)
	}
}

func TestLoadAndRefreshWithOrgPreservesOmittedRefreshToken(t *testing.T) {
	useTemporaryCredentials(t)
	var calls atomic.Int32
	server := tokenServer(t, &calls, "")
	defer server.Close()

	if err := SaveCredentials(&Credentials{
		AccessToken: "expired", RefreshToken: "refresh-old",
		ExpiresAt: time.Now().Add(-time.Hour).Unix(), OrgID: "org-1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAndRefreshWithOrg(server.URL, "org-1"); err != nil {
		t.Fatal(err)
	}
	creds, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if creds.RefreshToken != "refresh-old" {
		t.Fatalf("refresh token = %q, want preserved refresh-old", creds.RefreshToken)
	}
}

func TestLoadAndRefreshWithOrgSerializesConcurrentRotation(t *testing.T) {
	useTemporaryCredentials(t)
	var calls atomic.Int32
	server := tokenServer(t, &calls, "refresh-new")
	defer server.Close()

	if err := SaveCredentials(&Credentials{
		AccessToken: "expired", RefreshToken: "refresh-old",
		ExpiresAt: time.Now().Add(-time.Hour).Unix(), OrgID: "org-1",
	}); err != nil {
		t.Fatal(err)
	}

	const commandCount = 8
	errs := make(chan error, commandCount)
	var wg sync.WaitGroup
	for range commandCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := LoadAndRefreshWithOrg(server.URL, "org-1")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", calls.Load())
	}
}

func TestLoadAndRefreshWithOrgDoesNotEchoRejectedTokenBody(t *testing.T) {
	useTemporaryCredentials(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"rejected refresh-secret-value"}`))
	}))
	defer server.Close()

	if err := SaveCredentials(&Credentials{
		AccessToken: "access-old", RefreshToken: "refresh-secret-value",
		ExpiresAt: time.Now().Add(-time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAndRefreshWithOrg(server.URL, "org-1")
	if err == nil {
		t.Fatal("expected rejected refresh to fail")
	}
	if strings.Contains(err.Error(), "refresh-secret-value") {
		t.Fatalf("error exposed token response body: %v", err)
	}
	if creds, loadErr := LoadCredentials(); loadErr != nil || creds == nil {
		t.Fatalf("transient rejection removed credentials: creds=%v err=%v", creds, loadErr)
	}
}
