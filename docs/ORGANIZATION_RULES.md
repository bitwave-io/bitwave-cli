# Organization Categorization Rules

Status: implemented in the fork

Bitwave rules categorize recurring transaction patterns. Unlike a one-time
bulk categorization, an enabled rule continues to affect future organization
data, so rule creation uses the CLI mutation safety contract.

First run `bitwave org accounting status --json`. Rules require an accounting
connection and chart account destinations; if either is missing, the returned
prompt guides the LLM through external connection or verification of the
automatically provisioned manual setup. The CLI must not create a second manual
connection.

## Commands

```bash
bitwave rule list
bitwave rule get RULE_ID
bitwave rule create
bitwave rule recipes
bitwave rule metadata-guide
bitwave rule context
bitwave rule plan
bitwave rule apply
bitwave rule validate RULE_ID TRANSACTION_ID
bitwave rule enable RULE_ID
bitwave rule disable RULE_ID
bitwave rule delete RULE_ID
```

`rules` is accepted as an alias for `rule`.

The agent workflow prefers stable metadata or `methodId` conditions found in
representative transaction samples. `rule context` and `rule plan` preserve
those fields and return bounded `conditionCandidates`; Canton mappings in
`metadata-guide` are examples rather than the scope of metadata rules.

## Trade fees

Bitwave trades require a fee contact, but normally no fee category. The compact
`trade` preset enforces this contract: `feeContactId` is required,
`feeCategoryId` is omitted, and `autoCategorizeFee` defaults to `false`. The fee
therefore remains part of trade treatment for capitalization rather than being
automatically posted to a gas-fee expense category.

Do not generalize the gas-only rule to trades. A standalone gas-only transaction
requires both a fee category and fee contact; a trade fee requires the contact
without the category.

## Rule planning hierarchy

The recommended starting point for nearly every organization is the same three
enabled, priority-1 rules: organization-wide trade, internal transfer, and
gas-fee only. The CLI/LLM should suggest them immediately after the accounting
connection and Gas Fees resources are ready, create the missing rules in one
batch unless the user declines, and verify them with one list call. No full
transaction scan is needed for this starting set.

Rules normally run in the background approximately twice per day. Creation or
enablement is not immediate execution. After the default batch, trigger an
asynchronous run so processing begins sooner:

```bash
bitwave rule run --org ORG_ID --yes
```

For faster bounded processing, prefer Bulk Run with an explicit inclusive date
range:

```bash
bitwave rule bulk-run --org ORG_ID \
  --from-date YYYY-MM-DD --to-date YYYY-MM-DD --timeout 10m --yes
```

Bulk Run interprets dates in the organization timezone and sends
`executeUpdates=true` to `/org/{orgId}/rules/execute`. When omitted, the date
range defaults to `2000-01-01` through the current date in that timezone.

The response means the run was requested, not that every transaction has
already finished processing.

`rule run` is organization-wide and has no date-bound parameter. Do not use it
for a request limited to a historical cutoff unless the user explicitly
approves processing later transactions too. Bulk Run is date-bounded but may
return HTTP 403 when parallel rule processing is not enabled for the org.
The CLI waits up to ten minutes for acceptance by default. A timeout has
unknown acceptance state, so verify representative transactions or workflow
state before retrying rather than submitting duplicates blindly.

Large ranges can also fail with server `DEADLINE_EXCEEDED` or
`RESOURCE_EXHAUSTED`. Use `--chunk-months 1 --chunk-delay 2s` to submit
non-overlapping monthly windows sequentially. The result reports every window,
its acceptance latency, and stops at the first failure so gaps are explicit.

## Inflow and outflow discovery

Start with Bitwave's Transaction Summary aggregates instead of exporting an
entire transaction history to the LLM:

```bash
bitwave rule flows prioritize --org ORG_ID --from YYYY-MM-DD --to YYYY-MM-DD
bitwave rule flows analyze --org ORG_ID --source summary --wallet WALLET_ID \
  --from YYYY-MM-DD --to YYYY-MM-DD
```

Rank wallets by uncategorized volume, then inspect only recurring wallet-level
clusters. Direction is evidence, not an accounting conclusion: exchange,
bridge, DeFi, and internal movements must not be forced into revenue or expense.
When raw transaction search is required, the compact cluster includes up to
five `sampleExplorerLinks` from Bitwave's `txViewLink` field for targeted public
block-explorer inspection.

Transaction Summary may normalize non-EVM addresses. Because Solana and other
networks can be case-sensitive, the CLI marks such clusters with
`exactAddressRequired`, omits the address from `suggestedRule`, and requires the
exact full address from a representative raw transaction before rule creation.

Create approved chart accounts and contacts without returning the client's
entire resource list:

```bash
bitwave org accounting accounts create --org ORG_ID --accounting-connection Manual \
  --id ID --name NAME --type revenue --yes
bitwave org accounting contacts create --org ORG_ID --accounting-connection Manual \
  --id ID --name NAME --type Customer --yes
```

### Dust is not spam

Use `dust-inflow` only for priced, economically real, uncategorized,
single-token receipts after spam scoring. The preset requires a wallet, one
asset, and an inclusive maximum quantity because Bitwave rule comparisons use
asset units rather than a universal USD FMV threshold:

```bash
bitwave rule plan --preset dust-inflow --wallet-id WALLET_ID --asset ASSET \
  --max-asset-qty MAX_QUANTITY --accounting-connection-id Manual \
  --category-id DUST_INCOME_CATEGORY_ID --contact-id DUST_SENDER_CONTACT_ID \
  --fee-category-id GAS_CATEGORY_ID --fee-contact-id GAS_CONTACT_ID --enabled
```

Never apply one threshold across different tokens, and never use this preset
for verified spam, multi-token transactions, bridges, internal transfers,
DeFi, known counterparties, or failed pricing. Verified single-token spam
belongs in the score-before-ignore workflow.

If the trigger repeatedly returns a server-side error, use the **Run Rules**
action on Bitwave's Rules page. The CLI should clearly distinguish this trigger
failure from rule-creation failure.

Before applying, check for equivalent existing rules so rerunning onboarding
does not create duplicates. Leave wallet, asset, address, and date filters
empty. Trade uses the Gas Fees contact only and keeps
`ignoreFailPricing=false`; internal transfer and gas-only use both the Gas Fees
category and contact.

Start with Tier 1 organization-wide transaction-type rules: trade,
internal transfer, and gas-fee only. These normally have no wallet filter; one
rule applies across the organization's wallets. A wallet-specific version is
an exception, not the default.

Keep `ignoreFailPricing=false` on the trade rule. Failed-priced trade-like
transactions can be DeFi activity and should remain outside the generic trade
rule for review.

Then plan Tier 2 granular deposit/inflow and withdrawal/outflow rules.
As a general best practice, simple inflow/outflow rules should include a wallet.
When flow analysis supplies a stable wallet ID, retain it. Omitting wallet scope
broadens the rule to every wallet and should be a deliberate organization-wide
decision, not an LLM default.
Direction alone does not determine treatment, so these rules require more
transaction evidence and narrower conditions such as stable metadata, method
ID, address, asset, or—when genuinely relevant—wallet.

`bitwave rule recipes --json` exposes the same policy through
`planningHierarchy`, `planningTier`, and `defaultScope` for
LLM clients. These are advisory defaults; requested filters and settings are
retained with warnings.

## List rules without filling LLM context

The list command returns compact summaries and a maximum of 25 enabled rules by
default:

```bash
bitwave --quiet rule list --org ORG_ID --query ETH --json
```

Useful options:

```text
--query TEXT         name, ID, asset, wallet, address, direction, or action
--limit NUMBER       1 through 500
--include-disabled   include rules that are not currently running
--full               return the complete Bitwave rule objects
```

Use `--full` only when an LLM needs the tagged action details to adapt an
existing rule. The response also includes the organization's total rule count.

Backend operation:

```text
POST https://api4.bitwave.io/graphql-reports
GraphQL query: rules(orgId)
```

The deployed GraphQL schema does not accept a rule ID, name filter, or page
limit. `--query` and `--limit` therefore reduce the CLI/LLM output but cannot
reduce the backend request: Bitwave still returns every rule before the CLI
filters it. On large organizations this endpoint can be substantially slower
than transaction search or categorization mutations and needs a server-side
filtered lookup to improve further.

## Create a rule

The GraphQL `Rule!` input is a tagged union. The CLI accepts its complete JSON
shape rather than dropping advanced rule fields through a lossy universal
flag set.

Example structure for a simple categorization rule:

```json
{
  "transfer": {
    "name": "ETH activity to Sales",
    "priority": 1,
    "accountingConnectionId": "CONNECTION_ID",
    "action": {
      "type": "SimpleCategorization",
      "contactId": "CONTACT_ID",
      "categoryId": "CATEGORY_ID",
      "feeContactId": "FEE_CONTACT_ID",
      "feeCategoryId": "FEE_CATEGORY_ID",
      "ignoreFailPricing": false
    },
    "disabled": true,
    "coin": "ETH",
    "direction": "All",
    "allowMismatch": false,
    "autoCategorizeFee": true,
    "collapseValues": false,
    "multiToken": false
  }
}
```

For ordinary inflows and outflows, network fees are separate `FEE` lines. A
simple rule with `autoCategorizeFee: true` must use an explicit fee category
and fee contact—normally Gas Fees—not the primary Revenue or Expense category.
The agent-native planner rejects an omitted fee mapping rather than silently
copying the primary category/contact. Set `autoCategorizeFee: false` only when
another deliberate workflow will handle those fee lines.

The example deliberately uses `direction: All`. For an inflow-only or
outflow-only rule, copy the exact direction value used by a comparable rule
returned from `rule list --full`; the server remains authoritative for the
organization's deployed rule schema.

Keep `multiToken: false` for a rule intended to match a single asset such as
ETH. Set it to `true` only when the rule is deliberately expected to match
transactions containing multiple assets.

Preview the exact GraphQL variables without changing the organization:

```bash
bitwave --quiet rule create \
  --org ORG_ID --input rule.json --dry-run --json
```

Create the rule after reviewing the preview:

```bash
bitwave --quiet rule create \
  --org ORG_ID --input rule.json --yes --json
```

Safety behavior:

- `--yes` is required for every create.
- Missing `disabled` is changed to `disabled: true`.
- An input containing `disabled: false` is rejected unless `--enabled` is
  explicit.
- `name`, numeric `priority`, `accountingConnectionId`, and `action` are
  checked before sending the mutation.
- Exactly one top-level rule variant is allowed.

Backend operation:

```text
POST https://api-app.bitwave.io/graphql
GraphQL mutation: createRule(orgId, rule)
```

## Validate before enabling

Use a known matching transaction to test a created rule:

```bash
bitwave --quiet rule validate RULE_ID TRANSACTION_ID \
  --org ORG_ID --json
```

Backend endpoint:

```text
GET /orgs/{orgId}/transactions/{transactionId}/rules/{ruleId}
```

For the recommended one-command LLM workflow, built-in presets, ID fast path,
client-side batch format, and lifecycle operations, see
[`AGENT_RULE_WORKFLOW.md`](AGENT_RULE_WORKFLOW.md).

For inflow and outflow discovery, use `bitwave rule flows analyze`. Its default
`auto` source prefers Bitwave's Transaction Summary Interacting Addresses
aggregate and falls back to bounded transaction search when necessary. The
analysis considers uncategorized transactions only unless
`--include-categorized` is explicit. It stops treating additional history as
useful evidence after 100 matching uncategorized transactions in a cluster; it
does not require the LLM to read the organization's complete transaction
history. All address values are returned in full and must remain unabridged in
subsequent rule conditions.

`rule get RULE_ID` avoids the full list endpoint. Unfiltered lists use the
paginated query. Text searches still require a full scan because the backend
does not expose a text filter.
