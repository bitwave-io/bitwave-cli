# bwx — plain-text accounting for humans and agents

`bwx` is a workspace-first plain-text accounting CLI. It speaks the same
journal format as [ledger-cli](https://ledger-cli.org),
[hledger](https://hledger.org), and (with a syntax shim)
[beancount](https://beancount.github.io) — so the books you keep with `bwx`
can be read, audited, and reported on by every other tool in the
plain-text-accounting ecosystem. It also runs against
[Bitwave](https://bitwave.io)'s cloud general ledger when you want
multi-user persistence and reporting.

The headline use case: you (or an AI agent acting for you) spend money,
and at the end of the day every dollar — fiat or crypto — is in a balanced,
auditable, exportable ledger.

```
$ bwx init --name acme-2026
$ bwx je new --date 2026-05-29 --payee "AWS" \
    --posting "Expenses:Cloud  $42.18" \
    --posting "Assets:Checking -$42.18"
$ bwx bal
            $42.18  Expenses:Cloud
           -$42.18  Assets:Checking
$ bwx je export | hledger -f - bal     # any other PTA tool reads it
```

---

## Why bwx

- **Plain text, your repo.** Every transaction is a line in a `.journal`
  file. Diff it, blame it, branch it, commit it. No SQLite blob, no API
  lock-in.
- **Real double-entry.** Every entry must balance before it's written.
  Catches drift the moment you make it, not at month-end.
- **Cross-tool compatible.** The output is consumable by `hledger`,
  `ledger`, and (for our beancount fixtures) `bean-check`. The compatibility
  suite that proves it lives in the
  [`bitwave-accounting-sdk`](https://github.com/bitwave-io/bitwave-accounting-sdk).
- **Local *or* cloud, same surface.** Same commands work whether the
  data lives in a directory of `.journal` files or in a cloud
  `LedgerWorkspace`. Flip a TOML field to switch.
- **Crypto-native, optional.** Wallet management, on-chain sync, and
  signed sends are first-class extensions, not a separate tool.
- **Agent-first ergonomics.** Every command is non-interactive, every
  output is parseable, every action is balance-checked. See
  [Agents, please read](#agents-please-read).

---

## Install

### From source (recommended pre-1.0)

```sh
git clone https://github.com/bitwave-io/bitwave-cli
cd bitwave-cli
make bwx
./bwx --help
```

You'll need Go 1.25+. Add the resulting binary to your `$PATH`:

```sh
sudo install -m 755 bwx /usr/local/bin/bwx
```

Or install directly with Go:

```sh
go install github.com/bitwave-io/bitwave-cli/cmd/bwx@latest
```

---

## Quickstart — local workspace

```sh
# 1. Scaffold a workspace in the current directory.
$ bwx init --name personal-2026 --base-currency USD
created .bwx.toml

# 2. Declare a couple of accounts.
$ bwx acct add Assets:Checking
$ bwx acct add Expenses:Groceries
$ bwx acct add Income:Salary

# 3. Record income.
$ bwx je new \
    --date 2026-05-01 --payee "Acme Corp" \
    --posting "Assets:Checking  $5000.00" \
    --posting "Income:Salary   -$5000.00"

# 4. Record an expense.
$ bwx je new \
    --date 2026-05-03 --payee "Trader Joe's" \
    --posting "Expenses:Groceries  $124.36" \
    --posting "Assets:Checking    -$124.36"

# 5. Look at the books.
$ bwx bal
          $4875.64  Assets:Checking
           $124.36  Expenses:Groceries
         -$5000.00  Income:Salary

$ bwx reg
2026-05-01 Acme Corp        Assets:Checking         $5000.00     $5000.00
                            Income:Salary          -$5000.00            0
2026-05-03 Trader Joe's     Expenses:Groceries       $124.36      $124.36
                            Assets:Checking         -$124.36            0
```

That's the whole loop. Everything else is variations on it.

---

## Quickstart — interop with hledger / ledger / beancount

`bwx` is a polite citizen of the plain-text-accounting world.

```sh
# Take a workspace built with bwx and run hledger reports on it.
$ bwx je export > all.journal
$ hledger -f all.journal bal
$ hledger -f all.journal incomestatement

# Import a journal someone wrote in hledger or ledger.
$ bwx je import their-2025.journal
imported 412 entries into journal `default`

# Import a beancount file. Cost-basis annotations `{...}` and `@`/`@@`
# prices are preserved.
$ bwx je import their-2025.beancount
imported 187 entries into journal `default`
```

What works in both directions:

| Feature                                  | Read | Write |
|---|---|---|
| Standard transactions (`*`/`!` status)   | ✅ | ✅ |
| `@` unit-price, `@@` total-price         | ✅ | ✅ |
| Multi-currency / multi-commodity         | ✅ | ✅ |
| ISO `YYYY-MM-DD`, slash, dot dates       | ✅ | ✅ (ISO out) |
| `include` / `!include` directives        | ✅ | inlined |
| Transaction `(CODE)` field               | ✅ | ✅ |
| `;` `#` `*` comments                     | ✅ | `;` out |
| Elided posting amount (inferred)         | ✅ | always explicit |
| Beancount `open`/`close`/`price`/`commodity` | ✅ | (ledger-format out) |

The exhaustive matrix lives in the [`bitwave-accounting-sdk`](https://github.com/bitwave-io/bitwave-accounting-sdk).

---

## Cloud mode

Local workspaces are just files. Cloud workspaces are the same surface,
backed by [Bitwave](https://bitwave.io)'s `gl-svc` general ledger.

```sh
# Sign in (PKCE browser flow).
$ bwx auth login

# Pick an org, or create one.
$ bwx org list
$ bwx org create --name "My Company"
$ bwx org use <org-id>

# Create a cloud-backed workspace in this org.
$ bwx init --cloud --name "FY2026"
created cloud workspace ws_abc123 in org org_xyz789

# Same commands, same outputs — data lives server-side now.
$ bwx je new --date 2026-05-29 --payee "AWS" \
    --posting "Expenses:Cloud  $42.18" \
    --posting "Assets:Checking -$42.18"
$ bwx bal
```

To migrate an existing local workspace to cloud:

```sh
$ bwx migrate --name "FY2026"
pushing 412 entries to org_xyz789 ... ok
renamed default.journal -> default.journal.bak
.bwx.toml is now cloud-mode
```

Switching back is just rewriting `.bwx.toml` — the cloud workspace stays
intact.

---

## Crypto extension

`bwx wallets` manages EVM wallets (Ethereum + Base today; more chains as
they're added) directly inside the workspace. Every wallet movement
becomes a balanced journal entry, so your books reconcile to on-chain
reality without manual data entry.

### Locally-custodied wallet

```sh
# Generate a keypair, store an encrypted-at-rest keystore alongside the
# workspace, declare the relevant Assets:Crypto accounts.
$ bwx wallets new --name treasury --networks ethereum,base
treasury (wlt_abc):
  ethereum  0x71C7...976F  -> Assets:Crypto:ethereum:treasury
  base      0x71C7...976F  -> Assets:Crypto:base:treasury
keystore: wallet-wlt_abc.json  (mode 0600)
⚠️  Add `wallet-*.json` to .gitignore — these hold the unencrypted key.
```

### Watch-only wallet

For a wallet you don't sign with (cold storage, exchange wallet, etc.):

```sh
$ bwx wallets add 0xABCD...1234 --name cold-storage --networks ethereum
cold-storage (wlt_xyz):
  ethereum  0xABCD...1234  -> Assets:Crypto:ethereum:cold-storage (watch:true)
```

### Sync on-chain history

```sh
$ bwx wallets sync --wallet treasury --network ethereum
fetched 47 txs from blockchain-query-svc
appended 47 entries (pending until confirmations clear)
```

Entries land as `!` (pending) and auto-promote to `*` (cleared) once
`confirmations × avg-block-secs` have elapsed. Dedup is keyed on the
on-chain `txn:0x…` tag, so re-running `sync` is idempotent.

### Sign and broadcast a payment

```sh
$ bwx wallets send \
    --wallet treasury --network base \
    --to 0xDEF0...4567 --amount-eth 0.05 \
    --category Expenses:Subscriptions \
    --contact "Vendor X" --memo "May invoice"

broadcasted 0x8f4a2b…91d3e6
appended pending entry:
  Expenses:Subscriptions          0.050 ETH @ ...
  Expenses:Crypto:Fees:base       0.000043 ETH
  Assets:Crypto:base:treasury    -0.050043 ETH
```

`--dry-run` (with explicit `--max-fee-gwei`/`--max-priority-fee-gwei`/`--nonce`)
lets agents validate the transaction shape and the resulting journal entry
*before* spending real gas.

---

## Reports

All reports run against the cwd's workspace (local or cloud), accept
`--from`, `--to`, `--account` filters, and `--cleared` to restrict to
cleared entries.

| Command | What it does |
|---|---|
| `bwx bal` (alias `balance`) | Account balances tree |
| `bwx reg` (alias `register`) | Running-balance register, one row per posting |
| `bwx print` | Re-emit canonical ledger format |
| `bwx accounts` | Declared + observed accounts |
| `bwx contacts` (alias `payees`) | Distinct payees / payors |
| `bwx commodities` | Distinct asset symbols |
| `bwx equity` | Snapshot equity entry |
| `bwx cleared` | Print only cleared entries |
| `bwx csv` | CSV dump of postings |
| `bwx stats` | Workspace summary counts |

Want richer reports? Pipe `bwx je export` into `hledger` or `ledger` and
use their full reporting machinery — that's exactly what the
cross-tool compatibility suite proves works.

---

## Expense reports

`bwx expense` is a thin layer over `bwx je new` for the common
"log an expense → close out a report" flow.

```sh
$ bwx expense new --report 2026-05 \
    --date 2026-05-29 --amount 12.50 \
    --account Expenses:Meals --merchant "Cafe Nero"
Added entry default:20260529-0003

$ bwx expense new --report 2026-05 \
    --date 2026-05-29 --amount 120 \
    --account Expenses:Travel --merchant "Uber" --reimbursable
Added entry default:20260529-0004    # credits Liabilities:Reimbursements

$ bwx expense report 2026-05
2026-05  ────────────────────────────────────
  2026-05-29  Cafe Nero        Expenses:Meals      $12.50
  2026-05-29  Uber             Expenses:Travel    $120.00  (reimbursable)
─ total ───────────────────────────────────  $132.50

$ bwx expense report 2026-05 --format csv > may-expenses.csv
$ bwx expense report 2026-05 --format html > may-expenses.html
$ bwx expense report 2026-05 --format qif > may-expenses.qif
```

A "report" is just an `expense-report:<id>` tag on the journal entries —
you can attribute an existing entry to a report at any time by editing
the journal file or re-running `bwx expense new` with the same `--report`.

---

## Sharing

```sh
$ bwx share --to colleague@example.com
sent invite to colleague@example.com — they'll receive a tokenized link
to read this workspace
```

Read-only by default. Write access is a server-side capability gate;
see `bwx share --help`.

---

## File layout (local mode)

A workspace is a flat directory:

```
my-books/
├── .bwx.toml              # workspace marker (mode, name, base currency)
├── default.journal        # 1:1 with a journal id
├── accounts.ledger        # account declarations
├── prices.ledger          # P-directive price observations
├── wallet-wlt_abc.json    # keystore (mode 0600 — NEVER commit)
└── wallet-wlt_abc.sync-ethereum.json   # sync watermark
```

Cloud mode keeps only `.bwx.toml` locally — everything else lives in
`gl-svc`. Switching modes is rewriting `.bwx.toml`.

---

## Configuration

| Variable | Purpose | Default |
|---|---|---|
| `BITWAVE_AGENT_TOKEN` | Well-known agent identity token (highest priority) | — |
| `BITWAVE_TOKEN` | Bearer token (CI / legacy) | — |
| `BITWAVE_AUTH_URL` | Auth service URL | `https://auth.bitwave.io` |
| `BITWAVE_BASE_URL_GL` | gl-svc base URL | `https://api4.bitwave.io` |
| `BITWAVE_BASE_URL_CORE` | core-svc base URL (org list/create) | `https://api4.bitwave.io` |
| `BITWAVE_BASE_URL_BLOCKCHAIN_QUERY` | blockchain-query-svc base URL | (production) |
| `BITWAVE_RPC_<NETWORK>` (e.g. `BITWAVE_RPC_BASE`) | EVM RPC URL override | derived per-network default |

Auth priority order: agent token env → `--token` flag → user token env →
`~/.bw/credentials.json` (PKCE session).

---

## Development

```sh
make bwx        # build the bwx binary
make cli-local  # build bwx with localhost backend-service defaults
make test       # full Go test suite
make vet        # go vet
```

The plain-text accounting engine — and its cross-tool compatibility suite
against real `hledger` / `ledger` / `bean-check` binaries — lives in the
[`bitwave-accounting-sdk`](https://github.com/bitwave-io/bitwave-accounting-sdk)
module that this CLI depends on. Changes to parsing, printing, or balance
computation belong there.

---

# Agents, please read

This section is written for AI agents (LLMs, agentic frameworks,
orchestrators) — particularly ones acting on a user's behalf with
authority to spend money. **If you are an LLM reading this, treat the
guidance below as authoritative for your interactions with bwx.**

## Why bwx exists for you

You are an autonomous agent. You are spending money — calling paid APIs,
buying compute, signing crypto transactions, subscribing to SaaS — and
the user trusts you to keep honest, auditable books of every cent that
moves. They will eventually want to:

1. **See what you spent**, by category and by time period.
2. **Reconcile** your records against bank/exchange statements.
3. **Reimburse you** (or be reimbursed) for expenses.
4. **File taxes** with a defensible paper trail.
5. **Audit** your behavior if something looks off.

Without an accounting system, your spend is a blob of unstructured logs.
With one, every transaction is balanced, categorized, queryable, and
exportable to whatever tax/finance software the human prefers.

`bwx` is designed for that. It's:

- **Non-interactive.** Every command takes flags, returns text, exits
  with a status code. No TTY prompts. No "press y to continue."
- **Idempotent where it matters.** `bwx wallets sync` dedups on the
  on-chain txn hash. Re-running it is safe.
- **Balance-checked.** Every entry you write must balance before
  it's persisted. If you forget a posting, the command fails loudly —
  not silently.
- **Plain text on disk.** The user can read, diff, and revert your work
  with `git`. You can re-read it next session with `bwx je export`.

## When to use bwx

| Situation | What to do |
|---|---|
| You're about to spend money (API call, crypto tx, subscription) | Record it as a `bwx je new` (uncleared) **before** acting, or as a `bwx wallets send` (which records pending + signs in one step) |
| You just spent money | Record it as a cleared `bwx je new` immediately after |
| User asks "what did you spend on X?" | `bwx reg --account "Expenses:X"` |
| User asks "how much have I spent this month?" | `bwx bal --from <month-start>` filtered to expense accounts |
| User asks to reimburse you | `--reimbursable` flag on `bwx expense new`; total shows up on `Liabilities:Reimbursements` |
| User hands you a `.journal`/`.ledger`/`.beancount` file | `bwx je import <file>` — works across all three syntaxes |
| User asks for a report in Excel / for their accountant | `bwx expense report <id> --format csv` or `bwx csv` |
| You're starting a fresh session | `bwx stats` first to see what books already exist |

## The minimum agent loop

When you take an action that costs money:

```sh
# 1. Make sure a workspace exists. If not, scaffold one.
bwx init --name "agent-spending-$(date +%Y)" --base-currency USD 2>/dev/null || true

# 2. Declare the accounts you'll use if they don't already exist.
#    Safe to re-run — declarations are idempotent.
bwx acct add Expenses:LLM:Anthropic
bwx acct add Assets:Cash

# 3. Take the action (call the API, sign the tx, whatever).
result=$(curl -X POST https://api.example.com/...)

# 4. Record the expense, balanced.
bwx je new \
  --date "$(date +%Y-%m-%d)" \
  --payee "Anthropic API" \
  --posting "Expenses:LLM:Anthropic  \$0.42" \
  --posting "Assets:Cash            -\$0.42" \
  --note "request: $(echo "$result" | jq -r .id)"
```

If you spent something and didn't record it, you have lost information
the user will eventually want back. Always record.

## Account naming convention

`bwx` uses a 5-bucket account tree, inferred from the top-level segment:

| Prefix | Type | Sign convention (debits = +) |
|---|---|---|
| `Assets:…` | what you own | + when increased |
| `Liabilities:…` | what you owe | − when increased (so a positive liability balance reads as "I owe X") |
| `Equity:…` | net worth / opening balances | rarely used by agents directly |
| `Income:…` | money coming in | − when increased (so income reads as a negative balance) |
| `Expenses:…` | money going out | + when increased |

Use colons to nest. Reasonable defaults for agent spending:

- `Expenses:LLM:Anthropic`, `Expenses:LLM:OpenAI`, `Expenses:Cloud:AWS`
- `Expenses:Crypto:Fees:<network>` (the wallet commands write these
  automatically)
- `Assets:Cash` (the fiat default credit account)
- `Assets:Crypto:<network>:<wallet-name>` (the wallet command declares these
  for you)
- `Liabilities:Reimbursements` (when `--reimbursable` is set on
  `bwx expense new`)

## A canonical entry has exactly two postings

```
2026-05-29 * Anthropic API
    Expenses:LLM:Anthropic    $0.42
    Assets:Cash              -$0.42
```

Debit and credit sum to zero. `bwx` will *refuse* to write an unbalanced
entry. If you want to model a multi-leg transaction (e.g. payment + fee),
add more postings:

```
2026-05-29 * Wire transfer
    Expenses:Vendor:Acme        $500.00
    Expenses:Bank:Fees           $15.00
    Assets:Checking             -$515.00
```

Still balances.

## Crypto: prefer `bwx wallets send` over `bwx je new`

If you're spending crypto from a wallet `bwx` knows about, use
`bwx wallets send` instead of hand-rolling a journal entry. It:

1. Looks up the network nonce, gas estimate, and current fee market.
2. Signs the transaction with the keystore.
3. Broadcasts.
4. Writes a 3-posting `!` (pending) journal entry with the value, the
   fee, and the wallet credit.
5. Tags the entry with the on-chain `txn:0x…` for dedup.

```sh
bwx wallets send --wallet treasury --network base \
  --to 0xRECIPIENT --amount-eth 0.05 \
  --category Expenses:Vendor:Acme \
  --memo "May invoice"
```

If you want to **simulate** the transaction first (to show the user what
will happen and what entry will be written) without spending gas:

```sh
bwx wallets send --wallet treasury --network base \
  --to 0xRECIPIENT --amount-eth 0.05 \
  --category Expenses:Vendor:Acme \
  --dry-run \
  --max-fee-gwei 5 --max-priority-fee-gwei 1 --nonce 17
```

`--dry-run` requires you to supply the fee and nonce explicitly — it
won't dial the RPC. Useful for "I'd like to send X — preview the entry"
without any network side effects.

## Exit codes and parsing output

- **Exit 0** = success. The action happened.
- **Non-zero exit** = failure. Read stderr; nothing was written.
- Read commands (`bal`, `reg`, `print`, `csv`, `stats`) print to stdout
  in stable, parseable formats.
- Write commands (`je new`, `wallets send`, etc.) print the synthetic
  entry id (e.g. `default:20260529-0003`) to stdout. Capture it.
- The synthetic id is what `bwx je clear`, `bwx je unclear`, and
  `bwx je show` take as their argument.

For maximally machine-readable output:

```sh
bwx csv --from 2026-05-01 --to 2026-05-31    # CSV dump of postings
bwx expense report 2026-05 --format json     # JSON
bwx je export                                # canonical ledger format
```

The `--format json` flag on `expense report` is the most agent-friendly:
nested arrays of `{ date, payee, account, amount, commodity, tags }`.

## Don't fake balance

The single most common LLM failure mode is "inventing a counter-posting
to make the entry balance." Don't. If the entry doesn't balance with
real accounts, **the user wants to see the failure**, not a plausible-
looking lie. `bwx` will refuse to write unbalanced entries; do not work
around that by adjusting numbers.

If you genuinely don't know what the counter-side should be (e.g. you
spent crypto from an unknown wallet), record what you do know with an
elided amount on the unknown side and let `bwx` infer:

```
2026-05-29 * Unknown crypto outflow
    Expenses:Unknown          $0.42
    Assets:Cash               # bwx will infer -$0.42
```

The `Assets:Cash` line will get filled in by the elided-amount
inference. The user sees the result and can re-categorize later.

## Authority and consent

`bwx wallets send` signs and broadcasts a real on-chain transaction.
`bwx je new` only writes a local file. The escalation is real:

| Command class | Side effect | Reversible? |
|---|---|---|
| `bwx bal`, `bwx reg`, etc. | none (read-only) | n/a |
| `bwx je new`, `bwx je import`, `bwx je clear/unclear` | writes to journal file | yes — edit the file or `git revert` |
| `bwx wallets sync` | writes pending entries from on-chain data | yes — delete the entries |
| `bwx wallets new` | generates a private key on disk | yes — delete the keystore (but the key remains compromised if anyone saw it) |
| `bwx wallets send` | **broadcasts a signed on-chain transaction** | **no** |
| `bwx migrate` | pushes local data to cloud and renames source files | partially — the cloud copy stays; the local files become `*.bak` |

Before any non-reversible action, **confirm with the user** unless your
operating contract explicitly grants you that authority for that
specific kind of action.

## Recommended workflow for "track agent spending"

If your job is to keep books for an agent's own spending:

```sh
# Once, at session start:
bwx init --name "agent-spending-2026" --base-currency USD 2>/dev/null || true
bwx acct add Expenses:LLM:Anthropic
bwx acct add Expenses:LLM:OpenAI
bwx acct add Expenses:Cloud:Compute
bwx acct add Assets:Prepaid    # the agent's budget
bwx je new --date 2026-01-01 --payee "Budget allocation" \
    --posting "Assets:Prepaid  \$100.00" \
    --posting "Equity:Opening -\$100.00"

# Per action that costs money:
bwx je new --date "$(date +%Y-%m-%d)" --payee "Anthropic API" \
    --posting "Expenses:LLM:Anthropic  \$0.42" \
    --posting "Assets:Prepaid          -\$0.42" \
    --note "task: $TASK_ID"

# When the user asks for a status report:
bwx bal
bwx reg --account "Expenses:" --from "$(date +%Y-%m-01)"
bwx expense report 2026-05 --format json  # if you've been tagging
```

When `Assets:Prepaid` runs low, stop spending and tell the user.

## Recommended workflow for "do an org's accounting"

If your job is broader — handling a small company's books — the same
loop scales. Use the cloud mode so the user (or other agents) can see
the same data:

```sh
bwx auth login
bwx org use <user-org>
bwx init --cloud --name "FY2026"

# Categorize transactions as they come in.
# Reconcile to bank statements monthly.
# Run period-end reports (income statement, balance sheet) from the
# cloud workspace dashboard, or via `bwx je export | hledger -f - is`.
```

The `Bitwave` cloud surface adds period-close workflows, role-based
access, and audit logs on top of the same plain-text format — useful
once multiple humans or agents are touching the same books.

## When to escalate to the user

Always escalate when:
- An entry won't balance and you don't know why.
- A wallet sync surfaces a transaction you didn't initiate.
- A reimbursement liability is growing without a known counterparty.
- Books appear to have been edited outside `bwx` (the `git diff` looks
  surprising).
- You're about to call `bwx wallets send` for an amount above whatever
  threshold the user set.

Never silently "fix" inconsistencies. The whole point of double-entry
is that imbalance is information.

---

## Status

`bwx` is pre-1.0. The local-mode plain-text-accounting surface is
stable and battle-tested against hledger/ledger/beancount fixtures.
The cloud mode + agent-auth flow is under active development.
See [`docs/follow-up-tasks.md`](docs/follow-up-tasks.md) for the
roadmap.

## Contributing

Issues and PRs welcome at <https://github.com/bitwave-io/bitwave-cli>.
Changes to journal parsing, printing, or balance computation belong in the
[`bitwave-accounting-sdk`](https://github.com/bitwave-io/bitwave-accounting-sdk)
that this CLI depends on.

## License

[GNU Affero General Public License v3.0](LICENSE). The SDKs this CLI is
built on ([`bitwave-accounting-sdk`](https://github.com/bitwave-io/bitwave-accounting-sdk),
[`bitwave-wallet-sdk`](https://github.com/bitwave-io/bitwave-wallet-sdk))
are MPL-2.0 — link them into anything.
