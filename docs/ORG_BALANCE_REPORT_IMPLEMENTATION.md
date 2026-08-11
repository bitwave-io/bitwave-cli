# Organization Balance Report — CLI Implementation

Status: implemented and verified end to end on 2026-08-10

Branch: `design/cloud-org-readiness`

Test organization: `24371c55e25ae883fb58`

## Outcome

The CLI can now run and download a Balance Report from an existing Bitwave
organization's product data. The user does not need to copy product wallets or
transactions into the separate CLI ledger workspace.

```bash
bitwave report balance \
  --as-of 2026-06-30 \
  --group-by wallet \
  --out 2026-06-30-balance-by-wallet.csv
```

The command uses the existing OAuth login and selected organization. A separate
API key is not required.

## What was added

- `bitwave report list`
- `bitwave report balance`
- Organization resolution through `--org`, a bound cloud workspace,
  `BITWAVE_ORG_ID`, or the active organization
- Explicit protection against a bound-workspace/active-org mismatch
- Wallet or asset grouping
- Optional subsidiary filters
- Asynchronous polling with a 15-minute default timeout
- CSV output to stdout or an atomic `--out` file write
- A typed organization-report HTTP client, isolated from the ledger client
- Provenance on stderr so redirected CSV remains clean
- Tests covering request contracts, CSV conversion, errors, and credential
  isolation for external signed download URLs

## Production API behavior

The web application currently defaults to the production-stable V1 Balance
Report lifecycle:

1. `GET /reports/view?type=BalanceReport&orgId=...&endDate=...&groupBy=...`
2. `GET /v2/orgs/{orgId}/reports/{runId}?includeDownloadUrls=true`
3. Download the `links.results.href` file

The CLI matches that default. It never sends the Bitwave bearer token to an
external signed-download host.

The newer V3 lifecycle is also implemented behind `--report-api v3`:

1. `POST /v2/orgs/{orgId}/report-runs`
2. `GET /v2/orgs/{orgId}/report-runs/{runId}/status`
3. `GET /v2/orgs/{orgId}/report-runs/{runId}/download`

Production testing found a deployment gap: V3 start and status succeeded, but
both the result and download routes returned HTTP 404. For that reason, V1 is
the default until those V3 routes are deployed through the production gateway.

## Supported flags

```text
--as-of YYYY-MM-DD        required balance date
--group-by wallet|asset   default: wallet
--subsidiary ID           repeatable or comma-separated
--format csv              current output format
--out PATH                stdout when omitted
--org ID                  explicit organization override
--no-wait                 print the run ID and exit
--timeout DURATION        default: 15m
--report-api v1|v3        default: v1; V3 is preview
```

The V3 contract additionally accepts `--currency`, `--wallet`,
`--include-ignored`, `--exclude-nft`, and `--skip-pricing`. The CLI rejects
those flags with V1 rather than silently ignoring them.

## Verification performed

```bash
go test ./...
```

All packages passed.

An end-to-end report was run with:

```bash
bitwave report balance \
  --org 24371c55e25ae883fb58 \
  --as-of 2026-08-10 \
  --group-by wallet \
  --out 2026-08-10-bitwave-balance-by-wallet.csv
```

Observed result:

- report run ID: `3222a104-c575-45bd-8472-642b482ffb17`
- command exit status: `0`
- downloaded CSV size: `63,366` bytes
- CSV lines: `379`
- output columns include ticker, value, fiat value, wallet, wallet ID, wallet
  address, token details, blockchain, and subsidiary information

## Important distinction

`bitwave bal` remains a ledger-workspace calculation over journal postings.
It does not read Bitwave product wallets.

`bitwave report balance` is the organization Balance Report and is the command
a user should run for balances by wallet or asset as of a specific date.
