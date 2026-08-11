# Organization Categorization Rules

Status: implemented in the fork

Bitwave rules categorize recurring transaction patterns. Unlike a one-time
bulk categorization, an enabled rule continues to affect future organization
data, so rule creation uses the CLI mutation safety contract.

First run `bitwave org accounting status --json`. Rules require an accounting
connection and chart account destinations; if either is missing, the returned
prompt guides the LLM through external connection or manual Bitwave setup.

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

The response means the run was requested, not that every transaction has
already finished processing.

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
