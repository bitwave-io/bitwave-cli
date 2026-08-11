# Spam-token triage

Bitwave's public address service exposes token metadata by ticker:

```text
GET https://address-svc-utyjy373hq-uc.a.run.app/symbols/{SYMBOL}
```

The response can include the coin ID, network, contract address, canonical and
pricing symbols, and `spamScore`. Bitwave's operational spam threshold is
`spamScore >= 0.5`. A lower nonzero score is worth review but is not an
automatic ignore recommendation.

## Check supplied tickers in bulk

```bash
bitwave --quiet transaction spam check TUSD ETH SOL-USDC --json
```

For a large list, put newline- or comma-separated symbols in a file:

```bash
bitwave --quiet transaction spam check --file token-symbols.txt \
  --concurrency 20 --json
```

The CLI accepts up to 10,000 distinct symbols and performs bounded concurrent
lookups. Override the endpoint for testing with
`BITWAVE_ADDRESS_SERVICE_URL`. `--threshold` is available only for a
deliberate alternative policy.

## Analyze an organization

Run spam triage after the initial type-first categorization rules:

```bash
bitwave --quiet transaction spam analyze --org ORG_ID --json
```

The command:

1. Uses the transaction-search asset facet to discover assets present in
   unignored, uncategorized transactions. It does not download the full ledger.
2. Uses Transaction Summary's asset choices to map Bitwave asset IDs to ticker
   symbols.
3. Checks the distinct symbols concurrently with the address service.
4. Requires the returned `coinId` to match the transaction's `COIN.{id}`. A
   ticker match alone is not enough because symbols can collide across assets.
5. Fetches at most 100 matching transactions for each confirmed spam asset.
6. Returns a transaction ID only when every token-bearing line contains that
   same spam asset.

Categorized transactions are excluded unless the user explicitly directs the
LLM to include them with `--include-categorized`. A transaction containing both
a legitimate and spam token is never ignore-ready in this workflow.

The organization response reports clean lookups as a count instead of printing
thousands of clean token records. Full details are retained for spam candidates,
nonzero scores requiring review, unresolved symbols, and mismatched coin IDs.

## Bulk-ignore the eligible transactions

Preview the exact selection and mutation:

```bash
bitwave --quiet transaction spam bulk-ignore \
  --org ORG_ID --dry-run --json
```

Let the CLI perform the bulk ignore after review:

```bash
bitwave --quiet transaction spam bulk-ignore \
  --org ORG_ID --yes --json
```

This runs the same discovery and coin-ID validation itself; the LLM does not
need to copy transaction IDs between commands. The mutation uses Bitwave's
bulk transaction-state workflow.

If an asset plan has `transactionPageTruncated: true`, rerun bulk-ignore after
the first workflow finishes. Ignored transactions are excluded by default, so
the next bounded page becomes available without loading every match into one
LLM response.

Do not interpret an unresolved symbol as clean. Explain that the service could
not classify it and leave the transaction unchanged unless the user provides
another basis for ignoring it.
