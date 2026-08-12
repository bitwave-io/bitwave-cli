# Organization Transaction Mutations

Status: implemented in the fork

## Safety contract

These commands mutate product transactions in the selected Bitwave
organization. They do not edit the CLI's local ledger.

- Every write requires `--yes`.
- `--dry-run` prints the exact method, path, and body without sending it.
- `--json` emits a versioned success/error envelope for LLM callers.
- Transaction IDs are deduplicated before bulk requests.
- HTTP 429 and 5xx errors are marked retryable in JSON.
- Bulk partial failures exit non-zero and retain every per-transaction result.
- Ignore/unignore waits for an asynchronous server workflow unless `--no-wait`
  is explicit.

Always preview a generated mutation before executing it.

## LLM workflow: find, narrow, preview, confirm

Users do not need to know a transaction hash. `transaction search` accepts the
same useful dimensions as the transaction UI and returns only 25 matches by
default. Each match is a compact summary with at most five lines; pass `--full`
only when complete objects are needed. For example, find inflows from one
address in one wallet and asset:

```bash
bitwave --quiet transaction search \
  --org ORG_ID \
  --from 2026-07-01 --to 2026-07-31 \
  --wallet "Treasury" \
  --asset ASSET_ID \
  --from-address 0x000000234243234 \
  --operation Receive \
  --limit 25 --json
```

Available filters include `--from-address`, `--to-address`, `--address`
(either side), `--wallet`, `--asset`, `--type`, `--operation`, `--state`,
`--categorization`, `--reconciliation`, `--ignored`, `--transaction`, and up
to five `--search` tokens. Supply the returned `nextToken` through
`--next-token` to inspect another page.

An LLM should show the user a compact summary of matches, then pass only the
reviewed IDs to ignore or categorization commands. It should query
`categorization-options` with the user's words (for example `--query revenue`)
instead of displaying every category and contact.

## Ignore and unignore

For spam-token discovery and bulk ignore, see
[`SPAM_TOKEN_TRIAGE.md`](SPAM_TOKEN_TRIAGE.md). `transaction spam analyze`
checks uncategorized assets concurrently and `transaction spam bulk-ignore
--yes` performs the mutation. A transaction is eligible only when every
token-bearing line contains the same confirmed spam asset.

Preview:

```bash
bitwave --quiet transaction ignore TXN_ID_1 TXN_ID_2 \
  --org ORG_ID --dry-run --json
```

Execute:

```bash
bitwave --quiet transaction ignore TXN_ID_1 TXN_ID_2 \
  --org ORG_ID --yes --json

bitwave --quiet transaction unignore TXN_ID_1 TXN_ID_2 \
  --org ORG_ID --yes --json
```

Endpoint:

```text
POST /v3/orgs/{orgId}/transactions/bulk-state
```

Request transitions are `ignore` and `un-ignore`. `--bulk-action-id` supplies
an optional server idempotency key. If the server starts a Temporal workflow,
the CLI polls:

```text
GET /v3/orgs/{orgId}/transactions/bulk-state/{workflowId}
```

## Categorization discovery

```bash
bitwave --quiet transaction categorization-options --org ORG_ID --json
```

The default response returns accounting connections and category/contact
counts, but intentionally omits the potentially large choice arrays. Narrow
the choices before putting them into an LLM context:

```bash
bitwave --quiet transaction categorization-options \
  --org ORG_ID --query "staking" --json

bitwave --quiet transaction categorization-options \
  --org ORG_ID --accounting-connection CONNECTION_ID \
  --query "fee" --limit 50 --json
```

Filtered choices include category, contact, and accounting-connection IDs.
Categories and contacts carry their accounting connection IDs so an LLM can
avoid mixing records from different connections. Disabled choices are excluded
unless `--include-disabled` is explicit, and each choice array is capped by
`--limit` (default 100, maximum 500).

The response also describes the common single-categorization fields and the
numeric `categorizationMethod` values used by Bitwave. Type-specific line and
asset fields should be derived from `bitwave transaction get` and the selected
Bitwave categorization contract.

## Single categorization

Single categorization is a tagged union whose fields depend on the transaction
and selected type. To preserve Bitwave's complete contract, the CLI accepts a
JSON file rather than a lossy set of universal flags.

First load the complete transaction so the LLM can reason about its lines,
wallets, assets, existing state, and categorization:

```bash
bitwave --quiet transaction get TXN_ID --org ORG_ID --json
```

This is read-only and calls `GET /v3/orgs/{orgId}/transactions/{transactionId}`.

Preview:

```bash
bitwave --quiet transaction categorize TXN_ID \
  --input categorization.json --org ORG_ID --dry-run --json
```

Execute:

```bash
bitwave --quiet transaction categorize TXN_ID \
  --input categorization.json --org ORG_ID --yes --json
```

British spelling is accepted as the `categorise` alias.

Endpoint:

```text
PATCH /v3/orgs/{orgId}/transactions/{transactionId}
```

The CLI verifies that the input is a JSON object, has a supported `type`, and
has an `accountingConnectionId` where required. The server remains authoritative
for transaction-specific line, amount, wallet, exchange-rate, category, and
contact validation.

## Bulk categorization

The typed form supports Bitwave's bulk `multivalue`, `trade`, and `transfer`
contracts.

Trade example:

```bash
bitwave --quiet transaction bulk-categorize \
  --type trade \
  --transaction TXN_ID_1 --transaction TXN_ID_2 \
  --accounting-connection CONNECTION_ID \
  --fee-contact CONTACT_ID \
  --fee-category CATEGORY_ID \
  --dry-run --json
```

After reviewing the preview, replace `--dry-run` with `--yes`.

Multivalue additionally requires:

```text
--send-contact
--send-category
--receive-contact
--receive-category
```

For large LLM-driven cleanup, typed mode can discover IDs itself. The selection
is always date-bounded, wallet-scoped, limited to uncategorized and non-ignored
transactions, and further narrowed by `--asset` or `--ticker`:

```bash
bitwave --quiet transaction bulk-categorize \
  --type multivalue \
  --from 2026-01-01 --to 2026-07-31 \
  --wallet WALLET_ID --ticker SOL-USDC \
  --transaction-type Receive --operation DEPOSIT \
  --accounting-connection CONNECTION_ID \
  --fee-contact FEE_CONTACT_ID --fee-category FEE_CATEGORY_ID \
  --receive-contact CONTACT_ID --receive-category CATEGORY_ID \
  --send-contact CONTACT_ID --send-category CATEGORY_ID \
  --dry-run --json
```

The CLI paginates with up to 1,000 transactions per search request, applies
transaction type and operation checks locally and case-insensitively because
the server filters are not reliable for those labels, then submits bounded
batches. Its JSON result includes selection, mutation, verification, and total
durations plus selected, succeeded, failed, verified, and remaining counts.

Bitwave's multivalue bulk contract structurally requires both send and receive
fields. Even for a locally verified inflow-only population, an agent must
explain that the unused send fields remain present in the request and obtain
approval. Never imply that the API has a receive-only bulk contract.

Use `--overwrite` only when existing categorization should be replaced.

For advanced or mixed bulk requests, pass the complete API body:

```bash
bitwave --quiet transaction bulk-categorize \
  --input bulk-categorization.json \
  --org ORG_ID --dry-run --json
```

Endpoint:

```text
PUT /v3/orgs/{orgId}/transactions
```

British spelling is accepted as the `bulk-categorise` alias.

## Create transactions

Creation writes are guarded by the same `--dry-run` then `--yes` workflow.
Amounts are decimal strings, timestamps must be RFC3339, and simple/trade
requests require a caller-controlled `--system-id` so the LLM does not silently
invent or reuse a source identity.

Simple inflow preview:

```bash
bitwave --quiet transaction create simple \
  --org ORG_ID --wallet "Treasury" \
  --system-id llm-import-20260810-001 \
  --at 2026-08-10T12:00:00Z \
  --direction inflow --amount 1.25 --asset ETH \
  --from-address 0x000000234243234 \
  --dry-run --json
```

Use `--direction outflow` for a withdrawal. Optional fee, address, memo,
description, and blockchain ID flags are available. Categorization at creation
time requires all three of `--category`, `--contact`, and
`--accounting-connection` so records from different accounting connections
cannot be mixed accidentally.

Trade preview:

```bash
bitwave --quiet transaction create trade \
  --org ORG_ID --wallet "Exchange" \
  --system-id llm-trade-20260810-001 \
  --at 2026-08-10T12:00:00Z \
  --acquire-amount 1 --acquire-asset ETH \
  --dispose-amount 3500 --dispose-asset USDC \
  --fee-amount 5 --fee-asset USDC \
  --dry-run --json
```

This produces acquire/dispose rows, and an optional fee row, tied together by
one trade ID. It calls:

```text
POST /txns/orgs/{orgId}/transactions?immediate=true
```

Internal-transfer preview:

```bash
bitwave --quiet transaction create internal-transfer \
  --org ORG_ID \
  --from-wallet "Treasury" --to-wallet "Operations" \
  --at 2026-08-10T12:00:00Z \
  --amount 0.5 --asset ETH --memo "Treasury funding" \
  --dry-run --json
```

Internal transfers use Bitwave's atomic `CreateInternalTransfer` GraphQL
mutation at `POST /graphql`. The CLI resolves exact wallet names to IDs and
rejects a transfer whose source and destination resolve to the same wallet.

After the user reviews a preview, execute the identical command with
`--yes --json` in place of `--dry-run --json`.
