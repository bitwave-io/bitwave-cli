# Accounting setup before categorization

An organization needs an accounting connection and available chart of accounts
before an LLM can create meaningful categorization rules. Category IDs are
scoped to an accounting connection, so this step comes after wallets/data sync
and before transaction categorization.

## One fast readiness check

```bash
bitwave org accounting status --json
```

The response is deliberately compact:

- `choose_accounting_setup`: ask one question—connect the client's external
  accounting system, or create a manual Bitwave chart.
- `chart_of_accounts_required`: wait for the external chart to sync or import a
  manual chart.
- `ready_for_categorization_and_rules`: continue without another prompt.

`rule context` includes the same `accountingReadiness` object. An LLM should not
ask again after the organization is ready.

## External accounting system

Provider authorization and credentials remain in the Bitwave web application.
The LLM should direct the user to Accounting Connections, let them select and
authorize their provider, and then rerun:

```bash
bitwave org accounting status --json
```

The provider's chart should be created and maintained in that accounting
system, then synced into Bitwave. The CLI will not write manual accounts into an
external connection.

## Manual Bitwave chart

Create the manual connection:

```bash
bitwave org accounting manual create --yes --json
```

This operation is retry-safe: if a manual connection already exists, the CLI
returns `skipped_existing` with its ID.

Create one account:

```bash
bitwave org accounting accounts create \
  --accounting-connection CONNECTION_ID \
  --id 4000 \
  --code 4000 \
  --name "Revenue" \
  --type revenue \
  --yes --json
```

Or import a chart in one command:

```json
{
  "accounts": [
    {
      "connectionId": "CONNECTION_ID",
      "id": "1000",
      "code": "1000",
      "name": "Digital Assets",
      "type": "asset",
      "description": "Cryptocurrency and token holdings"
    },
    {
      "connectionId": "CONNECTION_ID",
      "id": "4000",
      "code": "4000",
      "name": "Revenue",
      "type": "revenue"
    },
    {
      "connectionId": "CONNECTION_ID",
      "id": "6100",
      "code": "6100",
      "name": "Crypto Fees",
      "type": "expense"
    }
  ]
}
```

```bash
bitwave org accounting accounts import --input accounts.json --dry-run --json
bitwave org accounting accounts import --input accounts.json --yes --json
```

Imports reuse one authenticated client and create up to eight accounts in
parallel. Supported account types are `asset`, `bank`, `equity`, `expense`,
`liability`, `other`, and `revenue`.

List bounded account choices without filling LLM context:

```bash
bitwave org accounting accounts list \
  --accounting-connection CONNECTION_ID \
  --query revenue --limit 20 --json
```

After setup, rerun status. When `readyForRules` is true, continue to transaction
analysis and rule planning.

## Important fee-policy distinction

Create a reusable fee contact for trade categorization. Trade fees require that
contact but normally do not use a fee category: the fee remains in the trade so
it can be capitalized. A standalone gas-only transaction is different and uses
both a gas-fee category and fee contact. The LLM must select the treatment from
the transaction type instead of assuming every fee takes the same category.
