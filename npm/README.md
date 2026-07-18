# bitwave

Agent-first accounting, by [Bitwave](https://bitwave.io) — run it locally,
share it in the cloud.

`bitwave` is a double-entry accounting CLI built for AI agents and the humans
who audit them: non-interactive commands, parseable output, balance-checked
writes. Books live in plain-text journals compatible with
[hledger](https://hledger.org), [ledger](https://ledger-cli.org), and (with a
shim) [beancount](https://beancount.github.io), with optional cloud
persistence and EVM wallet sync so on-chain activity lands in balanced,
auditable books.

```sh
npm install -g bitwave
bitwave init --name my-books
bitwave je new --date 2026-07-18 --payee "AWS" \
  --posting "Expenses:Cloud  $42.18" \
  --posting "Assets:Checking -$42.18"
bitwave bal
```

This package is a thin launcher: the actual Go binary for your platform is
pulled in via an optional dependency (`@bitwave-io/bitwave-<os>-<arch>`). No
install scripts run.

> **Note:** versions below 0.1.0 reserve the name ahead of the first binary
> release. If the launcher reports a missing platform package, update to the
> latest version or see the
> [other install methods](https://github.com/bitwave-io/bitwave-cli#install).

Source, issues, and docs: https://github.com/bitwave-io/bitwave-cli

License: AGPL-3.0-only.
