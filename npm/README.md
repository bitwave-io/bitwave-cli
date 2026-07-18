# wavie

Plain-text accounting for humans and agents, by [Bitwave](https://bitwave.io).

`wavie` is a workspace-first double-entry accounting CLI that speaks the same
journal format as [hledger](https://hledger.org),
[ledger](https://ledger-cli.org), and (with a shim)
[beancount](https://beancount.github.io) — with optional cloud persistence and
EVM wallet sync so your on-chain activity lands in balanced, auditable books.

```sh
npm install -g wavie
wavie init --name my-books
wavie je new --date 2026-07-18 --payee "AWS" \
  --posting "Expenses:Cloud  $42.18" \
  --posting "Assets:Checking -$42.18"
wavie bal
```

This package is a thin launcher: the actual Go binary for your platform is
pulled in via an optional dependency (`@bitwave-io/wavie-<os>-<arch>`). No
install scripts run.

> **Note:** versions below 0.1.0 reserve the name ahead of the first binary
> release. If the launcher reports a missing platform package, update to the
> latest version or see the
> [other install methods](https://github.com/bitwave-io/bitwave-cli#install).

Source, issues, and docs: https://github.com/bitwave-io/bitwave-cli

License: AGPL-3.0-only.
