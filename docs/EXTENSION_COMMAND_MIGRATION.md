# Extension command migration

This matrix records the CLI-native destination for every retained command from
the former browser extension. The CLI uses explicit nouns and verbs rather
than page-dependent slash commands.

| Extension action | CLI command | Coverage |
|---|---|---|
| `org id` | `bitwave org current` (`org id` alias) | Active organization ID/name |
| `users` | `bitwave org users` | Users, roles, email, last login |
| `wallets` | `bitwave org wallets list` | Includes network, address, created time, disabled state, and sync start |
| `wallets d` | `bitwave org wallets list --disabled-only` | Disabled-only filter |
| `disable wallets` | `bitwave org wallets disable … --yes` | Bulk enable/disable with dry-run and result envelope |
| `all tokens` | `bitwave org tokens list` (`tokens all` alias) | Organization lookup values, including spam tokens |
| `txn <id>` | `bitwave transaction get <id>` | Complete transaction JSON |
| `txn count` | `bitwave transaction count` | Date, wallet, asset, and ignored-status filters |
| `wallet export` | `bitwave org wallets export` | Date, wallet, asset, state, ignored, and other export filters |
| `export txns` | `bitwave report transaction-export` (`report export-txns` alias) | Validated server CSV export independent of browser UI state |
| `negatives` | `bitwave transaction negatives <wallet> <token>` | Exact-decimal running balance, first negative, lowest balance, optional CSV |
| `pricing report` | `bitwave pricing history` (`pricing report` alias) | Multi-token, maximum-31-day history with provenance and optional CSV |
| `spam` | `bitwave org tokens spam` | Metadata scoring/heuristics and confirmed Ignore-rule creation |
| `price` | `bitwave lookup price` | Public historical fiat price lookup |
| token lookup | `bitwave lookup token` | Public token metadata |
| `ca` | `bitwave lookup networks` (`lookup ca` alias) | Active networks for a contract address |
| network + token address | `bitwave lookup contract` | Network-specific contract details |
| `block` | `bitwave lookup block` | Block lookup by date/timestamp |
| `blocktime` | `bitwave lookup block-time` (`lookup blocktime` alias) | Block timestamp in seconds and UTC ISO format |
| `inventory view` | `bitwave inventory list` | Organization inventory views |
| `inventory updates` | `bitwave inventory updates <view>` | Runs, status, and errors |
| `connections` | `bitwave org connections` | ERP/exchange details and last sync state |
| `rules` | `bitwave rule …` | List/get/create/update/toggle/delete/validate/run/bulk-run |
| `balance report vs dashboard` | `bitwave report balance-compare` | Inventory dashboard versus Balance Report quantities |
| `resync wallet` | `bitwave org wallets resync` | Dry-run or confirmed full replay while preserving the current syncer version |
| `error` | `bitwave error` | Redacted most-recent CLI failure for support |
| `help` | `bitwave help [command]` | Whole CLI or command-family help |
| `info` | `bitwave info [--json]` | Complete live command/flag catalog |

The intentionally excluded extension commands are `clear`, `declare version`,
`v1`, `v2`, `bearer token`, `import`, `importV2`, `snake`, and `frankenstein`.
No top-level migration commands were added for them. Existing unrelated CLI
operations such as `bitwave org clear`, `bitwave je import`, and
`bitwave version` remain part of the original CLI and were not removed.
