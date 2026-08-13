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
With no connection, explain that the normally provisioned manual setup is
missing and ask the user to verify provisioning or connect their external
accounting system. Do not create another manual connection. Once connected, inspect that
connection's categories; do not assume a Digital Assets account exists. Ask
for the client's mapping when absent and warn before creating a possible
duplicate, but do not block an explicit request. When `readyForRules` is true, continue immediately without
asking again. `rule context` also embeds this readiness object.

If the user has not supplied a chart or contact list, use `bitwave org
accounting starter show --json` as the maximum default scope. The LLM may
recommend additions from transaction evidence, but must present them as a
proposal and recommend user approval. Accounting guidance is advisory: after
explaining the likely consequence, the CLI must allow the requested operation.

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

`gas-fee-only` has a specific Bitwave representation: Standard transaction type,
Outbound direction, and Advanced Categorize > Detailed Categorize with one line
using the `fee` value extractor and `COIN` asset extractor. The line posts to the
chosen gas category and contact. Do not model this preset as an internal transfer,
and do not use the disabled Fee Only transaction-type tab.

## Plan rules in hierarchy order

Rule planning starts with transaction type, not wallet. `rule recipes` returns
this as the machine-readable `planningHierarchy`.

### Always suggest the Tier 1 defaults first

As soon as the accounting connection, Gas Fees category, and Gas Fees contact
are available, the LLM should suggest starting with these three enabled rules:

1. All trades
2. Internal transfers
3. Gas-fee only

Unless the user declines or equivalent rules already exist, create the missing
rules together in one CLI batch. This setup does not require downloading or
analyzing the organization's transaction history. The fast sequence is:

1. List existing rules once.
2. Resolve or create the Gas Fees category and contact once.
3. Submit all missing Tier 1 rules through one authenticated `rule apply`
   process.
4. Verify the resulting rules once.
5. Trigger `bitwave rule run --yes` so the new rules begin processing without
   waiting for the background schedule.

Each default is enabled, organization-wide, and uses Bitwave rule priority `1`.
It has no wallet, asset, address, or date filter. The trade rule uses the Gas
Fees contact without a fee category and keeps `ignoreFailPricing=false`. The
internal-transfer and gas-only rules use both the Gas Fees category and contact.
Do not create duplicates when an equivalent rule is already present.

Only after this default batch should the LLM analyze transaction history for
Tier 2 deposit/inflow and withdrawal/outflow rules.

### Creation is not execution

Bitwave's background rule processing runs intermittently, approximately twice
per day. Creating or enabling a rule does not immediately apply it to existing
transactions. After creating the default batch—or whenever the user wants new
rules processed sooner—trigger the asynchronous organization-wide run:

```bash
bitwave --quiet rule run --org ORG_ID --yes
```

The command confirms that processing was triggered; it does not claim that the
entire transaction history has already finished. If an active or recently
completed run already exists, the server remains authoritative about whether a
new run starts.

### Triage spam tokens after rule setup

After the initial rules are created, use `bitwave transaction spam analyze` to
check distinct token symbols in uncategorized transactions. The command uses
Bitwave's address-service spam score, validates the returned coin ID against
the transaction asset, and emits bounded `ignoreTransactionIds` only when
every token-bearing line contains the same spam asset. Categorized transactions
remain out of scope unless the user explicitly requests
`--include-categorized`.

The LLM can execute the entire reviewed operation with `bitwave transaction
spam bulk-ignore --yes`. Never ignore a transaction containing both a
legitimate and spam token. See
[`SPAM_TOKEN_TRIAGE.md`](SPAM_TOKEN_TRIAGE.md) for the full workflow.

If the CLI trigger returns a persistent server-side error, explain that the
rules were created but have not been run yet, then direct the user to Bitwave's
Rules page and the **Run Rules** action. Do not imply that rule creation itself
processed the transactions.

Tier 1 contains the organization-wide type rules:

1. Trade
2. Internal transfer
3. Gas-fee only

Create one applicable rule of each type for the organization. Leave wallet,
asset, address, and date filters empty by default; do not create one trade,
internal-transfer, or gas-only rule per wallet. Add scope only for a deliberate
exception where the treatment genuinely differs.

Tier 2 contains deposits/inflows and withdrawals/outflows. Direction is not
enough to determine their accounting treatment. Inspect the transaction and
narrow the rule using stable metadata, method ID, address, asset, wallet, or
another supported condition. As a general best practice, simple inflow and
outflow rules should include a wallet. Keep the stable `walletId` returned by
the analyzer when available. An unscoped rule can affect every wallet and
should be used only when that organization-wide treatment is deliberate.

### Discover Tier 2 flows without reading the full ledger

Start with Bitwave's Transaction Summary dashboard data rather than exporting
or loading every transaction into the LLM:

```bash
bitwave --quiet rule flows analyze --org ORG_ID --json
```

In `auto` mode, the CLI first reads the same Interacting Addresses aggregate
used by Bitwave's Transaction Summary page. It groups inflows and outflows by
wallet and counterparty and returns the highest-count uncategorized
patterns. If that dashboard endpoint is unavailable, it falls back to a
bounded, paginated transaction search and reports the fallback in `warnings`.
Use `--source summary` or `--source transactions` only when a caller needs to
force one path.

The default scope is **uncategorized transactions only**. Do not include
already categorized activity in discovery or evidence unless the user directs
the LLM to do so; that explicit path is `--include-categorized`. The JSON
response identifies the active scope in `transactionScope`.

The LLM does not need exhaustive history. One hundred matching uncategorized
transactions for the same counterparty is sufficient evidence that a pattern
recurs. The response therefore reports `evidenceCount`, caps evidence retained
for a cluster at 100, and sets `evidenceSufficient=true` at that threshold. Raw
transactions should be inspected only when the aggregate does not explain the
business meaning; inspect no more than 100 representative uncategorized
matches before asking the user.

Every `counterpartyAddress`, `fromAddress`, and `toAddress` is emitted as the
complete exact value. Never shorten an address in a rule proposal, command, or
condition—even for display—and never convert `0x123456...abcdef` text copied
from a UI into a rule condition. Obtain the full value from the API first.

Treat each cluster as a question, not an accounting answer. Ask what the
counterparty activity represents, then resolve only the relevant category and
contact. A recurring address may span wallets, so do not add a wallet condition
unless wallet identity genuinely changes the required treatment.

`planningTier` describes the order in which an LLM should reason about rule
coverage. It is separate from Bitwave's numeric `priority` field; the CLI does
not silently rewrite the priority selected by the user.

### Trade fee treatment (required Bitwave behavior)

A trade rule requires a **fee contact** even though its primary trade does not
require a category or contact. Do not supply a fee category for a trade. Keep
`autoCategorizeFee: false`; this leaves the fee inside trade treatment so the
inventory calculation can capitalize it rather than posting it automatically
to a period expense.

Keep `ignoreFailPricing: false`—the "ignore failed pricing" checkbox remains
unchecked—for the organization-wide trade rule. Some transactions identified
as trades can represent DeFi activity. If pricing fails, the generic trade rule
should not sweep those transactions into automatic trade categorization. The
CLI warns if an LLM enables this setting but honors the explicit request.

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

## Blank transaction cleanup

Transactions that appear blank in the Bitwave UI commonly surface as
`transactionType=Unknown` with zero transaction lines and no asset, amount,
address, or wallet evidence. They can create substantial UI noise. Use the
`ignore-blank` preset only after inspecting representative examples:

```bash
bitwave --quiet rule apply \
  --preset ignore-blank \
  --name "Cleanup - Ignore Blank Transactions" \
  --priority 1 --direction Empty \
  --from-date YYYY-MM-DD --to-date YYYY-MM-DD \
  --accounting-connection-id CONNECTION_ID \
  --enabled --yes --org ORG_ID
```

A zero-line record can also mean transaction data is incomplete or missing, so
blank-ignore behavior is organization-specific and date-bounded rather than a
universal default. Run this specific rule before a broad priority-3 `All`
catch-all. `ignoreFailPricing=true` on a categorization rule is separate: it
allows booking unpriced economic activity and must never be used as evidence
that a transaction is blank.

## Optional catch-all clearing rule

A catch-all is a user choice, not an automatic onboarding step. When the user
wants one, use the dedicated `catch-all-clearing` preset. It defaults to
direction `All` and `multiToken=true`, and recommends priority 3:

```bash
bitwave --quiet rule apply \
  --preset catch-all-clearing \
  --name "Fallback - Remaining Transactions to Clearing" \
  --priority 3 \
  --accounting-connection-id CONNECTION_ID \
  --category-id CLEARING_CATEGORY_ID \
  --contact-id FALLBACK_CONTACT_ID \
  --fee-category-id GAS_CATEGORY_ID \
  --fee-contact-id GAS_CONTACT_ID \
  --enabled --yes --org ORG_ID
```

Before creating it, the LLM should prompt the user to confirm the fallback
treatment and check that enabled trade and internal-transfer rules have higher
precedence. This is advisory: an explicit user decision may proceed with a
different hierarchy.

The accounting consequence must be explained plainly. Trades should remain
trades. Transfers between owned wallets should normally carry assets at cost;
if a broad clearing, income, or expense rule captures them instead, Bitwave may
treat the movement like a disposal and calculate artificial gains or losses.
Specific gas-only, metadata, counterparty, wallet, and other approved rules
should also run before the fallback. Ask separately whether unpriced activity
should be included through `--ignore-fail-pricing`.

## Collapsing offsetting values

`collapseValues` is useful for certain multi-line transactions where the same
wallet has inbound and outbound lines for the same assets. Exact positive and
negative quantities can net to zero; unequal quantities can remain as a single
net line. The agent can set it explicitly:

```bash
bitwave --quiet rule plan \
  --preset internal-transfer \
  --multi-token --collapse-values \
  --accounting-connection-id CONNECTION_ID \
  --fee-category-id GAS_CATEGORY_ID \
  --fee-contact-id GAS_CONTACT_ID \
  --org ORG_ID
```

The LLM should inspect every line first. A good candidate is multi-line, often
multi-token, uses the same wallet on the relevant inbound/outbound lines, and
contains offsetting quantities for one or more assets. However, this shape does
not prove an internal transfer: trades, routed swaps, DeFi interactions,
bridges, and fee mechanics can also produce offsetting same-wallet lines.
Explain the proposed net result, ask when the economic meaning is ambiguous,
and validate a matching and non-matching transaction. Use
`--no-collapse-values` to retain individual values explicitly.

`bitwave rule recipes` exposes the same logic in the machine-readable
`valueHandling` object.

## Prefer metadata and method ID when transaction evidence supports them

Metadata rules are not Canton-only. An LLM should inspect representative
transaction data first and prefer stable, repeated metadata key/value pairs
wherever they express the intended activity. For repeated smart-contract or
DeFi interactions, it should also consider `methodId`. Wallet, address,
direction, and coin are useful narrowing conditions when metadata or a method
ID would otherwise match unrelated activity.

Start with the bounded analyzer rather than exporting full transaction
history:

```bash
bitwave --quiet rule metadata analyze --org ORG_ID --json
bitwave --quiet rule metadata analyze --org ORG_ID \
  --wallet WALLET_ID --from YYYY-MM-DD --to YYYY-MM-DD --json
```

It scans every transaction type and network, while defaulting to unignored,
uncategorized evidence. Each candidate includes count, coverage, exact
key/value, and its observed wallets, transaction types, networks, and assets.
Repeated stable conditions rank first. Known transaction-specific keys and
high-cardinality fields rank as unsafe. The LLM should use those scope fields
to decide whether wallet, address, asset, direction, or method ID is needed;
it must not assume that metadata alone determines the accounting treatment.

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

Canton uses a `partyId` where other networks use a counterparty address. The
CLI maps the complete exact party ID observed at runtime to `fromAddress` or
`toAddress`; it never abbreviates or normalizes it. No real client or vendor
party IDs belong in reusable CLI knowledge. The guide includes only the
generic terminology and documented system-side `0x0` pattern.

The guide also provides accounting decision support. Ownership, direction,
party ID, transaction type, and metadata help the LLM recommend an economic
account class: internal transfer, reward/revenue, network-fee expense,
reward-claim fee, application subscription expense, rebate/revenue-share
income, or needs review. The LLM should explain the evidence and resolve the
actual category/contact from the active organization's approved chart.

For Canton, treat `TransactionType` as a disambiguator rather than a complete
rule. The same metadata value can have different meanings: for example,
`FeeType=holding_fees` on `AmuletRules_Transfer` represents an idle-coin
transfer fee, while the same value on `AmuletRules_BuyMemberTraffic`
represents an idle-coin usage fee. Preserve exact live spellings; underscores
and hyphens are not interchangeable unless transaction evidence proves they
are normalized by the API.

`metadata-guide --chart both` also returns a machine-readable Canton playbook:

- fee, reward-fee, reward, wallet-program, and sweep rule archetypes;
- whether a category/contact is required;
- generic versus specific COA guidance;
- validation and overlap checks.

Example account names and numbers are mappings, not defaults. Resolve the
active organization's approved category and contact IDs. Confirmed sweeps use
internal-transfer categorization; rebates, subscriptions, minting fees, and
rewards remain client accounting decisions and must not be inferred from a
wallet name alone.

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
  --wallet-id WALLET_ID \
  --asset ETH \
  --from-address 0x1234 \
  --category "Sales (4000)" \
  --contact "Treasury" \
  --fee-category "Gas Fees" \
  --fee-contact "Gas Fees" \
  --enabled \
  --org ORG_ID
```

`plan` resolves exact names to stable IDs, returns representative transactions,
and prints the exact GraphQL rule envelope. It never changes the organization.
Its `scopeAssessment` is machine-readable: an unscoped simple inflow/outflow
plan reports `recommended=false` and `risk="broad-simple-flow"`. This is an
advisory override, not a blocker.

## Fast apply

When a preceding context/plan response already supplied stable IDs, use the ID
flags. This skips all discovery endpoints and sends only the rule mutation:

```bash
bitwave --quiet rule apply \
  --preset simple-inflow \
  --name "ETH inflows to Sales (4000)" \
  --wallet-id WALLET_ID \
  --asset ETH \
  --accounting-connection-id CONNECTION_ID \
  --category-id CATEGORY_ID \
  --contact-id CONTACT_ID \
  --fee-category-id GAS_CATEGORY_ID \
  --fee-contact-id GAS_CONTACT_ID \
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

Network fees on ordinary inflows and outflows are separate `FEE` transaction
lines. When a simple or metadata categorization rule uses the default
`autoCategorizeFee=true`, the CLI requires an explicit fee category and fee
contact—normally the client's Gas Fees selections. It never silently copies a
Revenue or General Expense category onto the fee line. Use
`--no-auto-categorize-fee` only when leaving fee lines for another deliberate
treatment.

## Client-side batch

`rule apply --input` accepts one spec or an array of up to 100 specs. Resources
are discovered once and the existing create mutation is called sequentially
with one cached org session:

JSON input follows the same priority default as the CLI flags: when `priority`
is omitted, the CLI sets it to `1`. An explicitly supplied value outside
Bitwave's `1`–`10` range—including `0`—is rejected instead of being sent to the
API. An LLM does not need to manufacture a priority merely to use batch input.

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
    "feeCategoryId": "Manual.gas",
    "feeContactId": "Manual.gas-vendor",
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

`--input` accepts a JSON file path, `-` for stdin, or inline JSON. A spec with
`id` is sent through Bitwave's `updateRule` mutation; a spec without `id` uses
`createRule`. The CLI never treats `createRule` as an upsert.

## Lifecycle commands

```bash
bitwave rule get RULE_ID
bitwave rule enable RULE_ID --yes
bitwave rule disable RULE_ID --yes
bitwave rule delete RULE_ID --yes
bitwave rule validate RULE_ID TRANSACTION_ID
bitwave rule run --org ORG_ID --yes
bitwave rule bulk-run --org ORG_ID --from-date YYYY-MM-DD --to-date YYYY-MM-DD --yes
```

Prefer `bulk-run` when the user can provide a bounded date range. It uses
Bitwave's faster Bulk Rules Run endpoint, interprets both dates in the
organization timezone, and applies enabled rules with `executeUpdates=true`.
When dates are omitted, it defaults to `2000-01-01` through the current date in
that timezone.
Use `run` only when the unbounded organization-wide trigger is intended.

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
