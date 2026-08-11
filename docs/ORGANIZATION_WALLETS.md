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

## Assess volume before adding anything

A wallet with millions of historical transactions should not be ingested as if
it were a normal treasury address. Large unrolled histories can be difficult to
process and expensive to correct after ingestion. The CLI therefore requires a
volume review before wallet creation.

The LLM should ask the user about wallet usage and expected volume. When
possible, it should independently estimate the address's transaction count for
the intended sync window using a block explorer, network indexer, or API. The
CLI does not pretend it can derive a reliable count uniformly across every
chain; it records and evaluates the evidence supplied by the user or LLM.

```bash
bitwave org wallets assess --input wallets.json --json
```

The response contains structured prompts, per-wallet readiness decisions, and
recommendations. The current high-volume threshold of 1,000,000 transactions is
an operational CLI heuristic, not a published Bitwave platform limit.

Every wallet input must include:

```json
"volumeReview": {
  "reviewed": true,
  "estimatedTransactions": 42000,
  "source": "block explorer",
  "evidence": "Explorer showed approximately 42k transactions on 2026-08-11"
}
```

If a reliable count cannot be found, the LLM must explain the uncertainty to
the user. Creation then requires `acknowledgeUnknown: true`. The CLI always
recommends speaking with Bitwave when volume or rollup design is uncertain.

### Explicit user override

The preflight is a guardrail, not a prohibition. If the user understands the
risk and still wants to ingest unknown or high volume without the recommended
Babel rules, record an explicit override:

```json
"volumeReview": {
  "reviewed": true,
  "estimatedTransactions": 20000000,
  "source": "explorer API",
  "evidence": "20,004,811 transactions in the requested window",
  "overrideRisk": true,
  "overrideReason": "User requires full unrolled history for an approved migration test"
}
```

Single-wallet mode uses `--override-volume-risk --override-reason "..."`.
The assessment and mutation JSON preserve the override and reason, and the
result reports `volumeRiskOverrides`. An override accepts only the
unknown/high-volume ingestion risk and the absence of recommended rollups. It
does not bypass volume review, malformed Babel rules, invalid wallet inputs, or
the Solana-validator configuration checks. The recommendation to consult
Bitwave still applies.

## Add one blockchain wallet

```bash
bitwave org wallets add \
  --name "Admin Wallet" \
  --address 0x15918ff7f6c44592c81d999b442956b07d26cc44 \
  --network polygon \
  --volume-reviewed \
  --estimated-transactions 42000 \
  --volume-source "block explorer" \
  --volume-evidence "approximately 42k transactions" \
  --yes
```

To proceed after an informed decision despite the volume safeguard, add:

```bash
  --override-volume-risk \
  --override-reason "User explicitly approved full unrolled ingestion"
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
    "networkId": "sol",
    "volumeReview": {
      "reviewed": true,
      "estimatedTransactions": 2500,
      "source": "user",
      "evidence": "bookkeeper estimate"
    }
  },
  {
    "name": "Art blocks",
    "address": "0x6C093Fe8bc59e1e0cAe2Ec10F0B717D3D182056B",
    "networkId": "eth",
    "subsidiaryId": "SUBSIDIARY_ID",
    "volumeReview": {
      "reviewed": true,
      "estimatedTransactions": 42000,
      "source": "block explorer",
      "evidence": "explorer count"
    }
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

## Modern Babel rollups

High-volume non-validator wallets require `babelRollupRules`. These are attached
immediately after `createWallet` through Bitwave's modern
`/orgs/{orgId}/wallets/{walletId}/rollup` endpoint. They are **not** the legacy
`accountBasedBlockchain.rollupConfig`.

Example for a wallet expected to import 20 million transactions:

```json
{
  "name": "High-volume payments",
  "address": "0x...",
  "networkId": "eth",
  "volumeReview": {
    "reviewed": true,
    "estimatedTransactions": 20000000,
    "source": "explorer API",
    "evidence": "20,004,811 transactions in the requested window"
  },
  "babelRollupRules": [
    {
      "ruleName": "Hourly incoming token payments",
      "classification": "incoming_token",
      "fingerPrint": "incomingToken",
      "rollupAction": "rollup",
      "cadence": "hour",
      "separateByInvolvedAccounts": false,
      "roundPeriod": "end-of-period"
    },
    {
      "ruleName": "Hourly fees",
      "classification": "fees",
      "fingerPrint": "onlyFee",
      "rollupAction": "rollup",
      "cadence": "hour",
      "roundPeriod": "end-of-period"
    }
  ]
}
```

Supported rule dimensions mirror the current Babel editor:

- fingerprint: `incomingNative`, `incomingToken`, `outgoingNative`,
  `outgoingToken`, `onlyFee`, `errored`, `any`, `simpleIncoming`,
  `simpleOutgoing`, or `simpleTrade`
- action: `rollup`, `rollupFromTo`, `rollupByAsset`, `nonRollup`, or `ignore`
- cadence: `hour`, `day`, or `month`
- optional metadata, involved accounts, counterparties, start/end times,
  account/counterparty separation, trade separation, and timestamp rounding

Rule ordering is significant. An LLM should design rules around the user's
actual transaction patterns and reporting needs, not blindly apply the example.
For a very large or unfamiliar wallet, review the design with Bitwave first.

Use these commands when inspection or after-the-fact correction is necessary:

```bash
bitwave org wallets rollup get "WALLET_NAME" --json
bitwave org wallets rollup set "WALLET_NAME" --input babel-rules.json --dry-run --json
bitwave org wallets rollup set "WALLET_NAME" --input babel-rules.json --yes --json
```

### Solana validator caveat

Solana validator transactions are rolled up automatically. Mark these inputs
with `"solanaValidator": true`; do not attach Babel rules. This exception applies
to Solana validators, not every Solana wallet.

## Waiting for wallet data

Creating a wallet confirms that Bitwave accepted the source; it does not mean
all historical transactions are immediately available. Wallet data typically
appears within **15 minutes**, but a large history or a busy network can take up
to **24 hours**.

Check one wallet without downloading its full history:

```bash
bitwave transaction search --wallet "WALLET_NAME" --limit 1 --json
```

A result with `count: 0` during that window means no indexed transactions are
available yet. It does not, by itself, mean wallet creation or sync failed. An
LLM should report the wallet as “waiting for data” and retry later. If it still
returns no expected data after 24 hours, investigate the wallet address,
network selection, and Bitwave sync status.

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
