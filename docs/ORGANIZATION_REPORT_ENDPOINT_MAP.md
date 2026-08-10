# Organization Report Endpoint and Naming Map

Status: implemented and production-tested on 2026-08-10

Branch: `design/cloud-org-readiness`

## Command naming policy

CLI commands use the report names a Bitwave user sees in the product. Aliases
may accommodate common shorthand, but they must not silently run a different
report.

| Bitwave product report | Canonical CLI command | API operation |
|---|---|---|
| Balance Report | `bitwave report balance` | Server-side Balance Report run and CSV download |
| Transaction Export | `bitwave report transaction-export` | Stream matching product transactions as CSV |
| Actions | `bitwave report actions` | Export Actions from one inventory view's active run |

Transaction Export aliases are `transactions-export` and `txn-export`.

`bitwave bal` is intentionally absent from this table. It is the CLI ledger
account-balance calculation, not Bitwave's product Balance Report.

## Balance Report

```bash
bitwave report balance \
  --as-of 2026-06-30 \
  --group-by wallet \
  --out balance.csv
```

The production-stable API lifecycle remains documented in
`ORG_BALANCE_REPORT_IMPLEMENTATION.md`.

## Transaction Export

```bash
bitwave report transaction-export \
  --from 2026-01-01 \
  --to 2026-06-30 \
  --out transactions.csv
```

Endpoint:

```text
POST /v3/orgs/{orgId}/transactions/export
```

This is the export companion to transaction search. It accepts the same filter
body but ignores pagination and streams every matching transaction as CSV.

### Date-range decision

- `--from` and `--to` are both required for a bounded export.
- Both dates are inclusive calendar days.
- The CLI reads the organization's timezone from `GET /v3/orgs/{orgId}` and
  sends it with the request, so dates match Bitwave rather than the user's
  workstation timezone.
- `--from` must be on or before `--to`.
- No dates are guessed or defaulted.
- An unbounded export requires the explicit `--all-dates` flag. It cannot be
  combined with `--from` or `--to`.

This avoids two bad defaults: silently exporting the org's entire history, or
silently choosing a calendar period the user did not request.

### Transaction Export filters

The CLI currently exposes server-backed filters for:

- wallet IDs;
- subsidiary IDs;
- asset IDs;
- transaction types;
- transaction states;
- categorization status;
- reconciliation status;
- ignored status;
- transaction ID/address search tokens (maximum five);
- combined transaction children.

Output is sorted chronologically by transaction timestamp. CSV can stream to
stdout or be written atomically with `--out`; a failed stream never leaves a
completed-looking destination file.

## Actions

First identify the inventory view:

```bash
bitwave report inventory-views
```

Then run the report:

```bash
bitwave report actions \
  --inventory-view "WLI simple" \
  --from 2026-01-01 \
  --to 2026-06-30 \
  --out actions.csv
```

Endpoints:

```text
GET /orgs/{orgId}/inventory-views
GET /orgs/{orgId}/inventory-views/{inventoryViewId}/actions
GET /v2/orgs/{orgId}/exports/{exportId}?rawUrl=true
```

### Inventory-view decision

`--inventory-view` is required and accepts either an exact ID or an exact,
case-insensitive name. The CLI does not choose the first or most recently used
view because the view determines the active inventory run, accounting method,
valuation configuration, and ultimately the report calculations. Ambiguous
names require an ID.

### Date-range decision

- `--from` maps to the endpoint's `startDate`.
- `--to` maps to the endpoint's `asOf` date.
- Both are required and inclusive.
- Bitwave evaluates them in the organization's timezone.
- A one-day report uses the same value for both.

### Actions filters

The CLI exposes the filters that the controller currently forwards into the
inventory query:

- inventory;
- subsidiary ID;
- action type;
- action status;
- transaction ID;
- asset ticker/name;
- asset ID;
- line error.

The backend controller currently declares a `wallet` query parameter but does
not forward it into `makeFilters`. The CLI deliberately does not expose a
`--wallet` flag for Actions because it would appear to work while being ignored.
This should be fixed server-side before adding the flag.

The export API may split large Actions reports into multiple CSV files. When
that happens, the CLI saves `-part-01`, `-part-02`, and so on rather than
discarding chunks or concatenating independently headed CSV files.

## Reports that are not aliases

The following Bitwave reports are related to Actions but are not interchangeable
with `Actions`, so the CLI must eventually give each its own explicit command:

- Actions Summary;
- Expanded Actions Report;
- Actions Journal Entry Report;
- Actions Trial Balance Report.

`bitwave report actions` only calls the product endpoint named `Actions`.

## Production verification

Organization: `24371c55e25ae883fb58`

Transaction Export test:

```bash
bitwave report transaction-export \
  --from 2026-06-30 --to 2026-06-30 \
  --out 2026-06-30-transaction-export.csv
```

Result: successful CSV download, 12 lines including the header. The resolved
organization timezone was UTC.

Actions test:

```bash
bitwave report actions \
  --inventory-view "WLI simple" \
  --from 2026-06-30 --to 2026-06-30 \
  --out 2026-06-30-actions-wli-simple.csv
```

Resolved inventory view ID: `hOFCZD3xoE47sgaWbrxo`.

Result: successful export lifecycle and signed-file download. The selected
view/date produced a header-only CSV, meaning there were no matching Actions;
the endpoint itself completed successfully.

All Go packages pass `go test ./...`.
