# Auth Package Assessment: `internal/auth`

The auth package is the most security-sensitive part of the CLI and is **generally
well-implemented**, with a few areas worth hardening.

## What's Good

- **Correct PKCE implementation.** 32 bytes of `crypto/rand` entropy, base64url-encoded verifier,
  SHA-256 challenge using the `S256` method. Meets [RFC 7636](https://datatracker.ietf.org/doc/html/rfc7636).
- **CSRF protection.** `state` parameter generated from `[A-Za-z0-9]{32}` with `crypto/rand`;
  validated on callback.
- **Restricted file permissions.** Credentials written with mode `0600`.
- **Loopback-only callback server.** Binds to `127.0.0.1`, not `0.0.0.0`.
- **Port cycling.** Tries 9180 → 9181 → 9182 before failing, avoiding single-port conflicts.
- **Auto-refresh with expiry buffer.** 60-second grace period before expiry triggers refresh.
- **Credential cleanup on refresh failure.** If refresh fails, stale credentials are deleted rather
  than left in an invalid state.

---

## Issues and Recommendations

| Severity | Finding | Location | Recommendation |
|---|---|---|---|
| **Medium** | `extractEmailFromIDToken` decodes the JWT payload but **does not verify the signature**. A tampered token would show a wrong email without error. | `login.go` | Use a JWT library to verify the ID token signature against the auth server's JWKS endpoint |
| **Medium** | Callback server has **no rate limiting or failed-attempt counter**. A local process could exhaust the code submission window. | `server.go` | Accept only one code; close the server on first valid or invalid submission |
| **Low** | PKCE challenge hashes the **raw 32 bytes** before base64-encoding, while the verifier is the base64url-encoded bytes. The RFC specifies `BASE64URL(SHA256(ASCII(verifier)))`. These currently agree only because base64url of raw bytes equals ASCII of the verifier, which is incidental. | `pkce.go` | Hash `[]byte(verifier)` instead of `rawBytes` for unambiguous RFC compliance |
| **Low** | `ClientCredentialsLogin` sends `client_secret` in the POST body. This is standard OAuth 2.0 but worth noting for audit purposes; no TLS pinning. | `credentials.go` | Document that transport-layer TLS must be enforced |
| **Low** | `RefreshTokens` deletes credentials on any non-200 response (network errors included), potentially forcing re-authentication unnecessarily. | `credentials.go` | Only delete credentials on explicit 400/401; retry or surface network errors separately |
| **Info** | 120-second callback timeout is reasonable but undocumented. | `server.go` | Surface to user as "Waiting 2 minutes for browser callback…" |

---

## Token Lifecycle

```
Login (PKCE)
  GeneratePKCE() → verifier + S256 challenge
  GenerateState() → 32-char CSRF token
  Start loopback server (127.0.0.1:9180-9182, 120s timeout)
  Open browser → auth.bitwave.io/authorize?code_challenge=...&state=...
  Receive callback → validate state → extract code
  ExchangeCode(code, verifier) → POST /api/oauth/token
  SaveCredentials() → ~/.bw/credentials.json (0600)

Each Command
  LoadCredentials()
  if IsExpired() → RefreshTokens() → POST /api/oauth/token
  return AccessToken

Logout
  DeleteCredentials()
  best-effort POST /api/oauth/logout
```
