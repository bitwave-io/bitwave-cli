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

`rule get RULE_ID` avoids the full list endpoint. Unfiltered lists use the
paginated query. Text searches still require a full scan because the backend
does not expose a text filter.
