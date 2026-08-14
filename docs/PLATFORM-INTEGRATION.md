# Bitwave platform (api-svc) vs. the workspace ledger (gl-svc)

There are **two completely different cloud surfaces**, and the CLI must never
conflate them:

| | **Bitwave platform** ("Bitwave proper") | **Workspace ledger** (alternate model) |
|---|---|---|
| Service | `api-svc` | `gl-svc` |
| Data model | Transactions, categorization, inventory views / lots / cost basis, report runs, wallets, connections | Workspaces → journals → plain-text-compatible journal entries (hledger/ledger/beancount) |
| Auth | **Client id / client key** → 1-hour Bearer JWT (see below); also user JWTs from the web app | OAuth PKCE session (auth-svc), agent tokens, `BITWAVE_TOKEN` |
| Routes | `/txns/…`, `/v2/orgs/{orgId}/report-runs`, `/orgs/{orgId}/inventory-views`, `/v3/orgs/{orgId}/transactions`, … | `/api/v1/orgs/{orgId}/ledger/workspaces/…`, `/v1/workspaces/…` |
| CLI today | **Not integrated at all** | Everything the CLI calls "cloud mode" (`init --cloud`, `je`, `bal`, `migrate`, `share`) |

Everything in this CLI that currently says "cloud" — cloud workspaces,
journals, `je`, reports against cloud workspaces — is the **gl-svc workspace
ledger**, i.e. the alternate plain-text-compatible model. It does **not** talk
to Bitwave proper. A daily close against Bitwave proper (transactions →
categorize → reports → inventory) is a different API with a different auth
model, mapped below from `~/Source/bitwave/api-svc`.

---

## Platform auth: client id / client key

Client credentials are **org-scoped API keys** (`ClientCredentialToken`:
`clientId`, `clientSecretHash`, `orgId`, `permissions: ReadAll|WriteAll`,
`enabled`). They are exchanged for a short-lived JWT — they are **not** sent
per-request.

```
POST https://api.bitwave.io/v2/oauth/token?grant_type=client_credentials
Content-Type: application/json (body fields: client_id, client_secret)

→ { "access_token": "<ES512 JWT>", "token_type": "Bearer", "expires_in": 3600 }
```

- v1 (`POST /oauth/token`, grant_type in body) exists but is deprecated.
- **No refresh token is ever issued.** Tokens live 1 hour; clients re-mint
  with the same client id/key. Any resolver must re-exchange on expiry, not
  attempt a `refresh_token` grant.
- The JWT `sub` is `{clientId}@clients.bitwave.io`; org access is encoded in
  scopes (`urn:orgs:{orgId}:…`). The org id also appears in every route path.
- All subsequent calls: `Authorization: Bearer <jwt>`.

Source: `api-svc/src/controllers/oauthController.ts`,
`api-svc/src/security/authTokenService.ts`, `api-svc/src/authentication.ts`.

### Known bug in this CLI

`bitwave auth login --client-id/--client-secret` posts a client_credentials
grant to **auth-svc** (`auth.bitwave.io/api/oauth/token`) — the wrong service —
and then assumes a refresh token exists. On first expiry the resolver runs a
`refresh_token` grant, fails, and **deletes credentials.json**
(`internal/auth/credentials.go`). Platform client credentials must instead be
kept and re-exchanged against `api.bitwave.io/v2/oauth/token` on demand.

---

## Platform endpoints for a daily close

All under `https://api.bitwave.io`, `Authorization: Bearer <jwt>`.

### Transactions

| Op | Endpoint | Notes |
|---|---|---|
| List | `GET /txns/{orgId}` | `pageLimit` (default 10), `paginationToken` cursor, `categorizationStatus=categorized\|uncategorized`, `includeExchangeRates` |
| Get one | `GET /txns/{orgId}/{transactionId}` | full `TransactionDTO` |
| **Categorize** | `PUT /txns/categorize/{orgId}/{transactionId}` | body is a `TransactionData` union — exactly one of `simple`, `sell`, `transfer`, `accountTransfer`, `staking`, `detailed`, `multivalue`, `invoice`, `trade`, `advanceDeFi`; optional `accountingConnectionId` |
| Create (batch) | `POST /txns/orgs/{orgId}/transactions` | `?immediate=true` for sync processing; per-row `persisted: created\|existing\|rejected` (closed periods reject) |
| Upsert | `PUT /txns/{orgId}/{transactionId}/{sourceId}` | `?uncategorize=true` supported |
| Export | `POST /v3/orgs/{orgId}/transactions/export` | filtered CSV export |

The daily-close triage query is
`GET /txns/{orgId}?categorizationStatus=uncategorized` — first-class on the
platform, impossible today in the gl-svc surface.

### Reports (async run model)

| Op | Endpoint |
|---|---|
| Start run | `POST /v2/orgs/{orgId}/report-runs` — body `{reportType, inputs[]}`; types incl. `BALANCE_REPORT`, `TRIAL_BALANCE`, `LEDGER`, `GAIN_LOSS` |
| Poll | `GET /v2/orgs/{orgId}/report-runs/{id}/status` (`PENDING/COMPLETED/FAILED`) |
| Result (JSON) | `GET /v2/orgs/{orgId}/report-runs/{id}` |
| Download (CSV) | `GET /v2/orgs/{orgId}/report-runs/{id}/download` |
| List runs | `GET /v2/orgs/{orgId}/report-runs?type=&limit=&pageToken=` |
| **Close report** | `POST /v2/orgs/{orgId}/close-reports` (PDF summary + exception-report links); `GET /v2/orgs/{orgId}/close-reports` to list |

Reports are **asynchronous**: start → poll → fetch. A CLI verb needs to wrap
that loop.

### Inventory

| Op | Endpoint |
|---|---|
| List views | `GET /orgs/{orgId}/inventory-views` |
| Balance (cost basis) | `GET /orgs/{orgId}/inventory-views/{viewId}/balance?asOf=&groupByInventory=&ignoreInternalTransfers=` |
| Lots | `GET /orgs/{orgId}/inventory-views/{viewId}/lots` |
| Gain/loss | `GET /orgs/{orgId}/inventory-views/{viewId}/gain-loss-summary` |
| Actions | `GET /orgs/{orgId}/inventory-views/{viewId}/actions` |

### Supporting

- Wallets: `GET /orgs/{orgId}/wallets` (`loadBalances=true`, cursor paging), `GET /orgs/{orgId}/wallets/{id}`
- Connections: `GET /orgs/{orgId}/connections?type=…`
- Prices: `POST /orgs/{orgId}/prices?fromAssetId=&toAssetId=&timestampSEC=&context=`

---

## Proposed CLI shape (not yet built)

Platform commands should live under an explicit namespace so the two surfaces
can never be confused — e.g.:

```
bitwave platform login            # client id/key → cached 1h JWT, auto re-mint
bitwave tx list --uncategorized   # GET /txns/{orgId}
bitwave tx show <id>
bitwave tx categorize <id> --simple ... | --file categorization.json
bitwave report run trial-balance --as-of 2026-07-24 [--wait --out tb.csv]
bitwave report close              # POST /v2/orgs/{orgId}/close-reports
bitwave inventory views
bitwave inventory balance [--view V] [--as-of D]
bitwave inventory lots [--view V]
```

Env: `BITWAVE_CLIENT_ID` / `BITWAVE_CLIENT_KEY` (platform naming is "client
key") read by the token resolver itself — mint on demand, cache in memory or
`~/.bitwave/platform-token.json`, re-mint on 401/expiry. Never route these
through auth-svc.
