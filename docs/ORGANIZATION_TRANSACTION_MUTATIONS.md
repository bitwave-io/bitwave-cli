# Organization Transaction Mutations

Status: implemented locally for testing; not pushed to GitHub or Bitwave upstream

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

## Ignore and unignore

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
POST /v3/orgs/{orgId}/transactions/bulk/state
```

Request transitions are `ignore` and `un-ignore`. `--bulk-action-id` supplies
an optional server idempotency key. If the server starts a Temporal workflow,
the CLI polls:

```text
GET /v3/orgs/{orgId}/transactions/bulk/state/{workflowId}
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
