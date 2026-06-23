package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// callbackResult holds the authorization code received from the OAuth callback.
type callbackResult struct {
	Code string
	Err  error
}

const callbackTimeout = 120 * time.Second

const successHTML = `<!DOCTYPE html>
<html><head><title>Bitwave CLI</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#f9fafb}
.card{text-align:center;padding:2rem;border-radius:8px;background:#fff;box-shadow:0 1px 3px rgba(0,0,0,.1)}
h1{color:#16a34a;margin:0 0 .5rem}p{color:#6b7280;margin:0}</style></head>
<body><div class="card"><h1>Authentication successful!</h1><p>You can close this tab.</p></div></body></html>`

const errorHTML = `<!DOCTYPE html>
<html><head><title>Bitwave CLI</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#f9fafb}
.card{text-align:center;padding:2rem;border-radius:8px;background:#fff;box-shadow:0 1px 3px rgba(0,0,0,.1)}
h1{color:#dc2626;margin:0 0 .5rem}p{color:#6b7280;margin:0}</style></head>
<body><div class="card"><h1>Authentication failed</h1><p>%s</p></div></body></html>`

// callbackPorts is the fixed set of ports the callback server will try.
var callbackPorts = []int{9180, 9181, 9182}

// startCallbackServer starts an HTTP server on 127.0.0.1 using one of the
// allowed callback ports (9180, 9181, 9182). It listens for a single
// GET /callback request, validates the state parameter, and returns the
// authorization code via the result channel.
func startCallbackServer(expectedState string) (port int, result <-chan callbackResult, err error) {
	var listener net.Listener
	for _, p := range callbackPorts {
		listener, err = net.Listen("tcp", fmt.Sprintf("localhost:%d", p))
		if err == nil {
			break
		}
	}
	if listener == nil {
		return 0, nil, fmt.Errorf("failed to start callback server: ports %v are all in use", callbackPorts)
	}

	port = listener.Addr().(*net.TCPAddr).Port
	ch := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			// Shut down the server in a goroutine so the response is flushed first.
			go func() {
				_ = srv.Shutdown(context.Background())
			}()
		}()

		// Validate state.
		state := r.URL.Query().Get("state")
		if state != expectedState {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, errorHTML, "State mismatch — possible CSRF attack. Please try again.")
			ch <- callbackResult{Err: fmt.Errorf("state mismatch: possible CSRF attack")}
			return
		}

		// Check for OAuth error response.
		if errCode := r.URL.Query().Get("error"); errCode != "" {
			desc := r.URL.Query().Get("error_description")
			if desc == "" {
				desc = errCode
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, errorHTML, desc)
			ch <- callbackResult{Err: fmt.Errorf("oauth error: %s — %s", errCode, desc)}
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, errorHTML, "No authorization code received.")
			ch <- callbackResult{Err: fmt.Errorf("no authorization code in callback")}
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, successHTML)
		ch <- callbackResult{Code: code}
	})

	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			ch <- callbackResult{Err: fmt.Errorf("callback server error: %w", err)}
		}
	}()

	// Timeout: shut down server if no callback is received.
	go func() {
		time.Sleep(callbackTimeout)
		_ = srv.Shutdown(context.Background())
		// Non-blocking send in case the channel already has a result.
		select {
		case ch <- callbackResult{Err: fmt.Errorf("timed out waiting for authentication (120s)")}: //nolint:mnd
		default:
		}
	}()

	return port, ch, nil
}
