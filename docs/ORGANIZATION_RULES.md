# Organization Categorization Rules

Status: implemented in the fork

Bitwave rules categorize recurring transaction patterns. Unlike a one-time
bulk categorization, an enabled rule continues to affect future organization
data, so rule creation uses the CLI mutation safety contract.

## Commands

```bash
bitwave rule list
bitwave rule create
bitwave rule validate RULE_ID TRANSACTION_ID
```

`rules` is accepted as an alias for `rule`.

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

Recommended LLM workflow:

1. Search a bounded transaction sample.
2. Query only relevant category/contact choices.
3. Create the rule disabled using `--dry-run`, then `--yes`.
4. Read the new rule ID with `rule list --include-disabled --query NAME`.
5. Validate it against one or more known transactions.
6. Only enable or run it after the user confirms the validation results and
   historical scope.

Rule execution and enable/disable mutations are not exposed yet because their
deployed API contracts were not present in the available source. The CLI does
not invent those operations.
