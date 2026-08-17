package cmd

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/bitwave-io/bitwave-cli/internal/auth"
	"github.com/bitwave-io/bitwave-cli/internal/orgctx"
)

// Persistent root flags.
var (
	authURLFlag string
	tokenFlag   string
)

// resolveAuthURL: --auth-url flag → BITWAVE_AUTH_URL env → default.
func resolveAuthURL() string {
	if authURLFlag != "" {
		return authURLFlag
	}
	if v := os.Getenv("BITWAVE_AUTH_URL"); v != "" {
		return v
	}
	return "https://auth.bitwave.io"
}

// defaultGLBaseURL is the build-time default for the cloud ledger. Production
// builds go through the api.bitwave.io gateway; local builds override via
// `-ldflags -X` (see the `cli-local` Makefile target). Always overridable at
// runtime with BITWAVE_BASE_URL_GL.
var defaultGLBaseURL = "https://api.bitwave.io"

// defaultCoreBaseURL is the build-time default for the Bitwave core API. Same override
// rules as defaultGLBaseURL.
var defaultCoreBaseURL = "https://api.bitwave.io"

// resolveIdentityEmail returns the logged-in user's email (the same value
// `bitwave auth status` displays), or "" when no email is resolvable — e.g.
// agent/bearer-token identities that carry no email claim. Never returns an
// error; a missing email is a normal, expected case.
func resolveIdentityEmail() string {
	// Env/flag token identities (agent, CI bearer) carry no email claim we can
	// read locally.
	if os.Getenv("BITWAVE_AGENT_TOKEN") != "" || tokenFlag != "" || os.Getenv("BITWAVE_TOKEN") != "" {
		return ""
	}
	creds, err := auth.LoadCredentials()
	if err != nil || creds == nil {
		return ""
	}
	return auth.ExtractEmailFromIDToken(creds.IDToken)
}

// resolveGLBaseURL: BITWAVE_BASE_URL_GL env → build-time default.
func resolveGLBaseURL() string {
	if v := os.Getenv("BITWAVE_BASE_URL_GL"); v != "" {
		return v
	}
	return defaultGLBaseURL
}

// resolveCoreBaseURL: BITWAVE_BASE_URL_CORE env → build-time default.
func resolveCoreBaseURL() string {
	if v := os.Getenv("BITWAVE_BASE_URL_CORE"); v != "" {
		return v
	}
	return defaultCoreBaseURL
}

// makeTokenResolver returns a token resolver applying the bitwave priority:
//
//  1. BITWAVE_AGENT_TOKEN env (well-known agent identity)
//  2. --token flag
//  3. BITWAVE_TOKEN env (legacy/CI)
//  4. ~/.bitwave/credentials.json (PKCE / delegated session, auto-refreshed)
//
// The first three are evaluated lazily so a value set by a parent command
// (or just-completed `bitwave auth login`) is picked up.
func makeTokenResolver() func() (string, error) {
	return func() (string, error) {
		if v := os.Getenv("BITWAVE_AGENT_TOKEN"); v != "" {
			return v, nil
		}
		if tokenFlag != "" {
			return tokenFlag, nil
		}
		if v := os.Getenv("BITWAVE_TOKEN"); v != "" {
			return v, nil
		}
		return auth.LoadAndRefresh(resolveAuthURL())
	}
}

// makeOrgTokenResolver wraps makeTokenResolver but exchanges for an
// org-scoped token when going through the credentials file. Static-token
// paths (env / flag) pass through unchanged.
func makeOrgTokenResolver(orgId string) func() (string, error) {
	// Org token exchange uses a rotating refresh token. A cloud command can
	// issue several API requests (for example Project loads workspace metadata,
	// accounts, entries, commodities, and prices), so exchanging on every
	// request needlessly rotates the refresh token and can invalidate the
	// credentials mid-command. Resolve lazily once and reuse the access token
	// for the lifetime of this command/client graph.
	var (
		once       sync.Once
		token      string
		resolveErr error
	)
	return func() (string, error) {
		once.Do(func() {
			if v := os.Getenv("BITWAVE_AGENT_TOKEN"); v != "" {
				token = v
				return
			}
			if tokenFlag != "" {
				token = tokenFlag
				return
			}
			if v := os.Getenv("BITWAVE_TOKEN"); v != "" {
				token = v
				return
			}
			token, resolveErr = auth.LoadAndRefreshWithOrg(resolveAuthURL(), orgId)
		})
		return token, resolveErr
	}
}

// requireActiveOrg loads the active org context, printing the standard hint
// if none is set. The hint mentions bitwave, not bw.
func requireActiveOrg() (*orgctx.Active, error) {
	a, err := orgctx.Load()
	if err != nil {
		if errors.Is(err, orgctx.ErrNoActiveOrg) {
			fmt.Fprintln(os.Stderr, "No active org. Run `bitwave org use` to pick one, or `bitwave org create` to make a new one.")
		}
		return nil, err
	}
	return a, nil
}
