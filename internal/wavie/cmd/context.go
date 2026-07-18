package cmd

import (
	"errors"
	"fmt"
	"os"

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

// defaultGLBaseURL is the build-time default for gl-svc. Production builds
// keep the api4 default; local builds override via `-ldflags -X` (see the
// `cli-local` Makefile target). Always overridable at runtime with
// BITWAVE_BASE_URL_GL.
var defaultGLBaseURL = "https://api4.bitwave.io"

// defaultCoreBaseURL is the build-time default for core-svc. Same override
// rules as defaultGLBaseURL.
var defaultCoreBaseURL = "https://api4.bitwave.io"

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

// makeTokenResolver returns a token resolver applying the wavie priority:
//
//  1. BITWAVE_AGENT_TOKEN env (well-known agent identity)
//  2. --token flag
//  3. BITWAVE_TOKEN env (legacy/CI)
//  4. ~/.wavie/credentials.json (PKCE / delegated session, auto-refreshed)
//
// The first three are evaluated lazily so a value set by a parent command
// (or just-completed `wavie auth login`) is picked up.
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
		return auth.LoadAndRefreshWithOrg(resolveAuthURL(), orgId)
	}
}

// requireActiveOrg loads the active org context, printing the standard hint
// if none is set. The hint mentions wavie, not bw.
func requireActiveOrg() (*orgctx.Active, error) {
	a, err := orgctx.Load()
	if err != nil {
		if errors.Is(err, orgctx.ErrNoActiveOrg) {
			fmt.Fprintln(os.Stderr, "No active org. Run `wavie org use` to pick one, or `wavie org create` to make a new one.")
		}
		return nil, err
	}
	return a, nil
}
