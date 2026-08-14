# Organization accounting data

`bitwave org accounting` exposes Bitwave accounting connections, chart
accounts, and categorization contacts. It reports backend state and performs
explicit mutations; it does not select an accounting policy or design a chart
of accounts.

## Inspect current state

```bash
bitwave org accounting status --json
bitwave org accounting connections list --json
bitwave org accounting accounts list --accounting-connection CONNECTION_ID --json
bitwave org accounting contacts list --accounting-connection CONNECTION_ID --json
```

`status` returns active connection and chart-account counts. Use `--query` and
`--limit` on account and contact lists to bound machine-readable output.

Provider authorization and credentials remain in the Bitwave web application.
Provider-owned charts should be maintained in the provider and synchronized to
Bitwave.

## Manual Bitwave connection

```bash
bitwave org accounting manual create --dry-run --json
bitwave org accounting manual create --yes --json
```

If a manual connection already exists, the operation returns
`skipped_existing` with its ID.

Create one manual account:

```bash
bitwave org accounting accounts create \
  --accounting-connection CONNECTION_ID \
  --id 4000 --code 4000 --name "Revenue" --type revenue \
  --yes --json
```

Import several manual accounts:

```json
{
  "accounts": [
    {
      "connectionId": "CONNECTION_ID",
      "id": "4000",
      "code": "4000",
      "name": "Revenue",
      "type": "revenue"
    }
  ]
}
```

```bash
bitwave org accounting accounts import --input accounts.json --dry-run --json
bitwave org accounting accounts import --input accounts.json --yes --json
```

Supported account types are `asset`, `bank`, `equity`, `expense`, `liability`,
`other`, and `revenue`.

`categories` is an alias for `accounts`. Match the web application's enabled
state controls with:

```bash
bitwave org accounting categories disable CATEGORY_ID --dry-run --json
bitwave org accounting categories disable CATEGORY_ID --yes --json
bitwave org accounting categories enable CATEGORY_ID --yes --json
bitwave org accounting categories disable-all --yes --json
```

Create one categorization contact:

```bash
bitwave org accounting contacts create \
  --accounting-connection CONNECTION_ID \
  --name "Counterparty" --type Vendor \
  --yes --json
```

A remote contact ID is optional, matching the web application. Optional
`--first-name`, `--last-name`, and `--email` fields are also supported.

Update the complete Bitwave `UpdateContactInput` contract without losing
address, metadata, default-category, or connection fields:

```bash
bitwave org accounting contacts update CONTACT_ID \
  --input contact-update.json --dry-run --json
bitwave org accounting contacts update CONTACT_ID \
  --input contact-update.json --yes --json

bitwave org accounting contacts disable CONTACT_ID --yes --json
bitwave org accounting contacts enable CONTACT_ID --yes --json
bitwave org accounting contacts disable-all --yes --json
```

The caller is responsible for choosing the connection, accounts, contacts, and
accounting treatment.
