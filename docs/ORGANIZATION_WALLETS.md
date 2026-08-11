# Organization wallets

`bitwave org wallets` manages wallets in the selected Bitwave product
organization. It is intentionally separate from the top-level `bitwave wallets`
command, which manages watch addresses for a local plain-text ledger workspace.

## Connect and inspect

```bash
bitwave auth login
bitwave org use ORG_ID
bitwave org current
bitwave org wallets list --json
bitwave org wallets networks --json
```

The network list contains the union of the current Add Source catalog and legacy
wallet adapter. Creation is forward-compatible: the CLI accepts a new canonical
network ID and lets the Bitwave API validate it, so adding a backend network does
not require an immediate CLI release.

## Add one blockchain wallet

```bash
bitwave org wallets add \
  --name "Admin Wallet" \
  --address 0x15918ff7f6c44592c81d999b442956b07d26cc44 \
  --network polygon \
  --yes
```

Use `--subsidiary SUBSIDIARY_ID` when applicable. Common names such as
`ethereum`, `solana`, `aptos`, and `bnb` normalize to Bitwave's canonical IDs.
Address casing is preserved.

## Add a batch

The batch format is designed for an LLM or onboarding script:

```json
[
  {
    "name": "Treasury",
    "address": "GQSQuaHPGiwAeYpQZXEwwrF7Sqek8vUR3oca34e4ocq7",
    "networkId": "sol"
  },
  {
    "name": "Art blocks",
    "address": "0x6C093Fe8bc59e1e0cAe2Ec10F0B717D3D182056B",
    "networkId": "eth",
    "subsidiaryId": "SUBSIDIARY_ID"
  }
]
```

```bash
bitwave org wallets add --input wallets.json --dry-run --json
bitwave org wallets add --input wallets.json --yes --json
```

Batch creation uses eight concurrent requests by default rather than paying one
round trip per wallet. Tune this with `--concurrency` (1–50) for the org and
network mix. Authentication is resolved once per batch; wallet sync remains an
asynchronous Bitwave service concern.

Before creating anything, the CLI validates supplied subsidiary IDs and checks
for an existing wallet with the same network and address. Existing matches are
reported as `skipped_existing`; use `--allow-duplicate` only when intentional.
If a batch fails partway through, the JSON response identifies created and failed
items so an agent can safely retry.

## Network-specific inputs

All normal blockchain addresses use Bitwave's modern
`accountBasedBlockchain` contract. Additional fields supported in batch JSON:

- `syncStartDateSEC`: earliest sync time as Unix seconds
- `isBalanceMonitoringOnly`: create without normal transaction processing
- `viewKey`: private-chain/view-key input such as Aleo
- `metadata`: network-specific input such as XRP destination tags
- `addressType: "hd"`: BTC or DASH xpub/derivation key; uses the legacy watch
  shape

Canton wallets automatically receive the same syncer-version configuration as
Bitwave's Add Source flow.

The API's `WalletInput` currently has no creation-time `description` field. A
non-empty `description` is rejected explicitly rather than silently discarded.
