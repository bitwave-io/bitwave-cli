# Agent-Native Categorization Rules

This workflow lets an LLM learn Bitwave rule semantics and create one or many
rules without a long sequence of independent CLI calls. The embedded recipes
are derived from Bitwave's official rule guide and the deployed rule input
contract. They are versioned, compact, and available without authentication.

Source: https://docs.bitwave.io/docs/set-up-categorization-rules

Metadata source: https://docs.bitwave.io/docs/metadata-based-rule-categorization

Method ID source: https://docs.bitwave.io/docs/how-to-use-rules

Recipe schema: `1`

Last verified: `2026-08-11`

## Confirm accounting readiness first

Before analyzing categories or creating rules, run:

```bash
bitwave --quiet org accounting status --json
```

If `interactionRequired` is true, ask the single prompt returned by the CLI.
With no connection, ask whether to connect the client's external accounting
system or create a manual Bitwave connection. Once connected, recognize that
Bitwave supplies Digital Assets automatically and ask only for missing
client-specific categories or contacts. Never create a duplicate Digital
Assets account. When `readyForRules` is true, continue immediately without
asking again. `rule context` also embeds this readiness object.

See `docs/ORGANIZATION_ACCOUNTING.md` for manual connection and chart import.

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
- `metadata-categorization`

### Trade fee treatment (required Bitwave behavior)

A trade rule requires a **fee contact** even though its primary trade does not
require a category or contact. Do not supply a fee category for a trade. Keep
`autoCategorizeFee: false`; this leaves the fee inside trade treatment so the
inventory calculation can capitalize it rather than posting it automatically
to a period expense.

```text
Trade action
  feeContactId: required
  feeCategoryId: omitted
  autoCategorizeFee: false
```

This is different from a standalone gas-only transaction, whose rule requires
both a fee category and fee contact. It is also distinct from the
internal-transfer recipe, which has its own fee-category/contact contract.

`detailed-categorization` is documented but deliberately remains raw-input
only. Detailed extractor lines are transaction-specific, so the CLI will not
guess them.

## Prefer metadata and method ID when transaction evidence supports them

Metadata rules are not Canton-only. An LLM should inspect representative
transaction data first and prefer stable, repeated metadata key/value pairs
wherever they express the intended activity. For repeated smart-contract or
DeFi interactions, it should also consider `methodId`. Wallet, address,
direction, and coin are useful narrowing conditions when metadata or a method
ID would otherwise match unrelated activity.

Compact `transaction search`, `rule context`, and `rule plan` samples retain
both `metadata` and `methodId`. `rule context` and `rule plan` also return
`conditionCandidates` with counts, coverage, sample transaction IDs, and an
assessment. Transaction-specific keys such as hashes, block numbers,
timestamps, IDs, and nonces are explicitly marked as unsuitable for reusable
rules.

The metadata guide returns this general decision policy plus Bitwave's
documented Canton vocabulary and chart mappings as examples:

```bash
bitwave --quiet rule metadata-guide --json
bitwave --quiet rule metadata-guide --key FeeType --chart specific --json
bitwave --quiet rule metadata-guide \
  --key RewardType --value input_app_reward_amount --json
```

The Canton examples include `FeeType`, `RewardFeeType`, `RewardType`, and
`TransactionType`, but custom keys observed in any client's transactions can
be used. Metadata and method ID conditions can be added to any supported
recipe:

```bash
bitwave --quiet rule apply \
  --preset metadata-categorization \
  --name "Protocol deposits" \
  --metadata protocol=Aave \
  --method-id 0xe8e33700 \
  --metadata-operator AND \
  --accounting-connection-id CONNECTION_ID \
  --category-id CATEGORY_ID \
  --contact-id CONTACT_ID \
  --enabled --yes --org ORG_ID
```

Repeat `--metadata` to combine multiple pairs. Valid operators are `AND`, `OR`,
`NAND`, `NOR`, and `XOR`. JSON plans use:

```json
{
  "metadata": [
    {"key": "protocol", "value": "Aave"}
  ],
  "metadataOperator": "AND",
  "metadataTransactionRecord": false,
  "methodId": "0xe8e33700"
}
```

For vendor-specific `TransactionType=Amulet_Rules Transfer` rules, metadata is
not sufficient to choose the accounting treatment. Add the relevant vendor
`fromAddress` or `toAddress` and let the user select whether that relationship
represents revenue share or a subscription fee. The official page marks its
metadata-based internal-transfer section as under construction, so the CLI
does not invent those mappings.

The transaction-search endpoint accepts `methodId` but has no metadata filter.
Samples from
`rule context` and `rule plan` are therefore approximate when metadata is
present, and the JSON response says so explicitly. Use Bitwave's exact rule
validation after a rule ID is available.

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
document. The LLM should inspect `conditionCandidates` before proposing a
broader address-only or asset-only rule.

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
  },
  {
    "preset": "metadata-categorization",
    "name": "Canton validator rewards",
    "accountingConnectionId": "Manual",
    "categoryId": "Manual.402",
    "contactId": "Manual.validator",
    "metadata": [
      {"key": "RewardType", "value": "input_validator_reward_amount"}
    ],
    "metadataOperator": "AND",
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
