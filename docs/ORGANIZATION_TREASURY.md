# Organization treasury monitoring

`bitwave treasury` manages Bitwave Treasury monitoring for the active
organization: wallet balance threshold monitors, the alerts they fire, alert
delivery channels, and the analytics query templates behind dynamic
thresholds. It calls the same backend operations as the Treasury pages of the
web application.

## Commands

| Command | Endpoint (all under `https://api.bitwave.io`) |
|---|---|
| `bitwave treasury monitors list` | `GET /v3/orgs/{org}/monitoring-configs` |
| `bitwave treasury monitors get MONITOR_ID` | `GET /v3/orgs/{org}/monitoring-configs/{id}` |
| `bitwave treasury monitors create ...` | `POST /v3/orgs/{org}/monitoring-configs` |
| `bitwave treasury monitors update MONITOR_ID ...` | `PUT /v3/orgs/{org}/monitoring-configs/{id}` |
| `bitwave treasury monitors delete MONITOR_ID` | `DELETE /v3/orgs/{org}/monitoring-configs/{id}` |
| `bitwave treasury monitors history MONITOR_ID [--days N]` | `GET /v3/orgs/{org}/monitoring-configs/{id}/balance-history?days=N` |
| `bitwave treasury monitors reevaluate [MONITOR_ID...] [--all]` | `GET` then `PUT /v3/orgs/{org}/monitoring-configs/{id}` (see below) |
| `bitwave treasury alerts list [--status S] [--tier T]` | `GET /v3/orgs/{org}/alerts?status=&tier=` |
| `bitwave treasury alerts ack ALERT_ID` | `POST /v3/orgs/{org}/alerts/{id}/acknowledge` |
| `bitwave treasury channels get` | `GET /v3/orgs/{org}/alert-channels` |
| `bitwave treasury channels set ...` | `PUT /v3/orgs/{org}/alert-channels` |
| `bitwave treasury templates list [--tag T] [--all]` | `GET /v3/orgs/{org}/analytics/templates?tags=treasury` |
| `bitwave treasury templates get TEMPLATE_ID` | `GET /v3/orgs/{org}/analytics/templates/{id}` |
| `bitwave treasury templates execute TEMPLATE_ID` | `POST /v3/orgs/{org}/analytics/queries/execute` |

`monitor`, `alert`, `channel`, and `template` are accepted as aliases; `alerts
acknowledge` and `templates run` are aliases too. Monitor, alert, and channel
routes are served by treasury-svc; template routes by analytics-query-svc.
Both are reached through the core API gateway, so `--org` and authentication
work as for every other organization command.

List commands print a table by default and a `{"schemaVersion":"1", ...}`
envelope with `--json`. `get` commands print the backend object as JSON.

Permissions are the org-scoped `treasury-monitoring` scopes: `read` for reads,
`create`/`update`/`delete` for monitor writes, and `update` for alert
acknowledgement and channel changes.

## Writes

Every write supports `--dry-run`, which prints the exact method, URL, and JSON
body without sending anything, and requires `--yes` to execute. With `--json`,
successes and failures use the standard mutation envelope (`status`,
`operation`, `organization`, `request`, `result` or `error`).

```bash
bitwave treasury monitors create --wallet WALLET_ID --wallet-name "Ops ETH" \
  --network eth --address 0xabc... --currency-id 10 \
  --approaching 5 --breaching 2 --dry-run --json
bitwave treasury monitors create ... --yes --json
```

## Monitors

A monitor watches one wallet currency, identified by `--currency-id` and
`--currency-type` (`Coin`, the default, or `Fiat`) the same way the balance
stream keys balances. The CLI does not resolve tickers or wallet metadata:
pass `--wallet-name`, `--network`, and `--address` (as shown by
`bitwave org wallets list`) so alerts display them.

Each tier (`approaching`, `breaching`) is one of:

- **STATIC**: `--approaching VALUE`. Fires when `$balance <= VALUE`.
- **DYNAMIC**: any of `--<tier>-template`, `--<tier>-predicate`, or
  `--<tier>-param` makes the tier dynamic. The predicate references the
  template's output columns and `$balance`, for example
  `'$balance < $total_earned * 0.702'`. `--<tier>` then sets the optional
  seed/display value (default 0).

`--<tier>-param key=value` is repeatable. A value that is a JSON literal keeps
its JSON type (`limit=10` sends a number, `flag=true` a boolean,
`code="10"` a string); anything else is sent as a string.

Client-side validation mirrors treasury-svc: STATIC values must be finite and
zero or greater, breaching must be strictly below approaching when both tiers
are STATIC, and a DYNAMIC tier needs a predicate. Unlike the backend, which
treats a missing value as 0, the CLI requires an explicit value for every
STATIC tier, including in `--input` bodies. Predicate syntax is parsed by the
backend.

`create` defaults to `alertEnabled: true` and `notifyOn: [APPROACHING,
BREACHING]`, matching the Treasury UI (the backend default for an empty
`notifyOn` is breaching-only). Override with `--alert-enabled=false` and
`--notify-on BREACHING`.

`update` sends only what you pass. Any flag for a tier replaces that whole
tier. When only one tier is given, treasury-svc validates the resulting pair
against the stored other tier. The wallet and currency of a monitor cannot be
changed.

Both commands accept `--input FILE` (or `-` for stdin) with a complete request
body instead of flags. Input is decoded strictly: unknown fields, such as
server-populated `lastStatus` from `monitors get`, are rejected.

## Re-evaluating monitors

treasury-svc evaluates monitors when balances change and on a schedule (up to
every 10 minutes), and has no dedicated evaluate endpoint. The web
application's "Re-evaluate dynamic thresholds" button runs each dynamic
tier's template through `POST /analytics/queries/execute` and evaluates the
predicate in the browser only. Nothing is persisted.

`bitwave treasury monitors reevaluate` instead re-saves each monitor's current
thresholds unchanged (`PUT`). treasury-svc evaluates a monitor right after
every write, so this refreshes and persists `lastStatus`/`lastBalance` and the
rule results. Consequences to be aware of:

- dynamic tiers execute their templates, which consumes query credits;
- a status change can fire or resolve alerts and send notifications;
- each call records a threshold-update event on the monitor.

Server-populated rule fields (`lastResult`, `lastEvaluatedAt`, `lastError`)
are stripped from the request. `--all` selects every monitor with a DYNAMIC
tier; explicit IDs may name any monitor. `--dry-run` performs the read-only
`GET` requests needed to build the exact `PUT` bodies and sends no `PUT`.

## Alerts

```bash
bitwave treasury alerts list --status OPEN --tier BREACHING
bitwave treasury alerts ack ALERT_ID --yes
```

`--status` accepts `ACTIVE`, `ACKNOWLEDGED`, `RESOLVED`, or `OPEN` (active or
acknowledged). Filters are applied by treasury-svc. Acknowledging a resolved
alert returns HTTP 409.

## Alert channels

```bash
bitwave treasury channels get
bitwave treasury channels set --channel EMAIL \
  --target "ops@example.com,cfo@example.com" --dry-run
```

`set` upserts the named channel; other channels are unchanged. The stored
target is replaced by `--target`, so `--target` is required even when only
disabling a channel (`--enabled=false`). An enabled EMAIL channel needs at
least one valid address. SLACK and WEBHOOK settings are stored but not yet
delivered. `--input FILE` accepts a `{"channels":[...]}` body for several
channels at once.

## Analytics templates

`templates list` returns templates tagged `treasury` by default, as the
Treasury UI's "Dynamic (query)" picker does. `--tag` (repeatable) and `--all`
change the filter. A template's `outputColumns` are the `$variables` a
predicate may reference, and its `parameters` are the `--<tier>-param` keys.

`templates execute` runs a template with `--param key=value` exactly as the
UI's re-evaluation does. It consumes query credits (HTTP 402 when the
organization's budget is exhausted), so it is treated as a write: it requires
`--yes` and supports `--dry-run`. A query above the service's large-query
threshold returns HTTP 409 with an estimate; re-run with
`--confirm-large-query`.

## Not exposed

- analytics-query-svc `POST /templates` (create), `POST /queries/estimate`,
  `GET /queries/{id}`, and `GET /budget`: the Analytics pages use them, but
  Treasury does not.
- analytics-query-svc `/internal/budgets/*`: service-to-service only, not
  routed through the public gateway.
- treasury-svc `/health`.
