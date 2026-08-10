# Agent-Native Categorization Rules

This workflow lets an LLM learn Bitwave rule semantics and create one or many
rules without a long sequence of independent CLI calls. The embedded recipes
are derived from Bitwave's official rule guide and the deployed rule input
contract. They are versioned, compact, and available without authentication.

Source: https://docs.bitwave.io/docs/set-up-categorization-rules  
Recipe schema: `1`  
Last verified: `2026-08-11`

## Let the LLM learn the supported patterns

```bash
bitwave --quiet rule recipes --json
bitwave --quiet rule recipes trade --json
```

Supported compact apply presets:

- `simple-inflow`
- `simple-outflow`
- `trade`
- `internal-transfer`
- `gas-fee-only`
- `ignore-blank`

`detailed-categorization` is documented but deliberately remains raw-input
only. Detailed extractor lines are transaction-specific, so the CLI will not
guess them.

## Discover only relevant choices

When names or the intended transaction scope are unclear, use one context
command instead of separate wallet, category, contact, and transaction calls:

```bash
bitwave --quiet rule context \
  --preset simple-inflow \
  --asset ETH \
  --from-address 0x1234 \
  --query revenue \
  --sample-limit 5 \
  --org ORG_ID
```

The command loads organization resources concurrently and returns bounded
wallet, category, contact, connection, and transaction samples in one JSON
document.

## Resolve a plan without writing

```bash
bitwave --quiet rule plan \
  --preset simple-inflow \
  --name "ETH inflows from treasury to revenue" \
  --asset ETH \
  --from-address 0x1234 \
  --category "Sales (4000)" \
  --contact "Treasury" \
  --enabled \
  --org ORG_ID
```

`plan` resolves exact names to stable IDs, returns representative transactions,
and prints the exact GraphQL rule envelope. It never changes the organization.

## Fast apply

When a preceding context/plan response already supplied stable IDs, use the ID
flags. This skips all discovery endpoints and sends only the rule mutation:

```bash
bitwave --quiet rule apply \
  --preset simple-inflow \
  --name "ETH inflows to Sales (4000)" \
  --asset ETH \
  --accounting-connection-id CONNECTION_ID \
  --category-id CATEGORY_ID \
  --contact-id CONTACT_ID \
  --enabled --yes \
  --org ORG_ID
```

For a single-asset rule, the preset emits `multiToken: false`. Trade emits
`multiToken: true`. Internal-transfer enables multi-token handling only when
the caller explicitly supplies `--multi-token`.

An explicit imperative user request such as “create this enabled rule” gives
the LLM enough authority to include `--yes`; the LLM does not need to ask the
same confirmation again. Ambiguous scope, categories, contacts, or historical
date coverage still require clarification.

## Client-side batch

`rule apply --input` accepts one spec or an array of up to 100 specs. Resources
are discovered once and the existing create mutation is called sequentially
with one cached org session:

```json
[
  {
    "preset": "trade",
    "name": "All trades",
    "accountingConnectionId": "Manual",
    "feeContactId": "Manual.1",
    "enabled": true
  },
  {
    "preset": "ignore-blank",
    "name": "Ignore blank transactions",
    "accountingConnectionId": "Manual",
    "enabled": false
  }
]
```

```bash
bitwave --quiet rule apply --input rules.json --org ORG_ID --yes
```

A dedicated backend bulk mutation is not required for this to be useful. It
would reduce HTTP calls for very large batches, but the CLI already avoids
repeated authentication and discovery.

## Lifecycle commands

```bash
bitwave rule get RULE_ID
bitwave rule enable RULE_ID --yes
bitwave rule disable RULE_ID --yes
bitwave rule delete RULE_ID --yes
bitwave rule validate RULE_ID TRANSACTION_ID
```

`rule get` uses the exact rule query and does not download the complete rule
collection. An unfiltered `rule list` uses the paginated API. A text search
must still scan all rules because the deployed API has no server-side text
filter.

## Backend follow-ups

The most valuable future Bitwave API changes are:

1. Return `ruleId` from `createRule`.
2. Add server-side name/ID/action filters to the paginated rule query.
3. Add a bulk create mutation only if real workloads show sequential client
   batching is insufficient.

Until creation returns an ID, fast apply reports success without performing a
slow full-list lookup. Exact server validation can be run once the caller has a
rule ID.

