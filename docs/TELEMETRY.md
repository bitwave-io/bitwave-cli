# Telemetry

The bitwave CLI collects **anonymous usage telemetry** so we can see which
commands humans and agents actually use, and where they hit errors.

## What is collected

One event per CLI invocation:

| Field | Example | Notes |
|---|---|---|
| `schema` | `cli-command/v1` | wire-format version |
| `ts` | `2026-07-19T01:12:03Z` | UTC, RFC3339 |
| `anonymousId` | `9f2c…` (32 hex chars) | random, generated locally; **never** derived from hardware, usernames, or network identity |
| `version` | `0.2.3` | CLI version |
| `os` / `arch` | `darwin` / `arm64` | |
| `installChannel` | `brew` \| `npm` \| `direct` | inferred from the binary's path |
| `command` | `je new` | command path only |
| `flags` | `["date","payee","posting"]` | flag **names** only — never values |
| `durationMs` | `42` | wall time of the invocation |
| `ok` | `true` | whether the command succeeded |
| `errorClass` | `usage` \| `api_4xx` \| `api_5xx` \| `other` | coarse class only — never error text |
| `agentTokenEnv` | `true` | whether `BITWAVE_AGENT_TOKEN` is set (presence only) |
| `tty` | `false` | whether stderr is a terminal |
| `harness` | `claude-code` | known agent-harness env markers, presence only |

## What is never collected

Argument **values** of any kind: payees, amounts, account names, journal
contents, file paths, wallet addresses, emails, tokens, error message text,
environment variable values. Telemetry has no access to ledger data by
construction — it sees the command line's *shape*, not its content.

## Opting out

No prompt is ever shown (bitwave sessions are commonly non-interactive).
A one-time notice is printed to stderr on first use. Opt out any of these
ways:

```sh
bitwave telemetry disable    # persisted; also wipes unsent local events
BITWAVE_TELEMETRY=0          # per-process env
DO_NOT_TRACK=1               # cross-tool convention, also honored
```

`bitwave telemetry status` shows the current decision and why. Dev builds
never send telemetry.

## Mechanics

Events append to a local spool (`~/.bitwave/telemetry-spool.jsonl`) and are
POSTed in batches after a command finishes — never on the critical path,
with a 2.5s timeout, silent on failure. The spool is capped at 200 events
(oldest dropped). Disabling wipes the spool.

---

## Ingest endpoint contract (server side)

The CLI POSTs to `https://api.bitwave.io/metrics`
(override for testing: `BITWAVE_TELEMETRY_URL`).

```
POST /metrics
Content-Type: application/json
(no authentication — events are anonymous by design)

{"events": [ <Event>, ... ]}       # 1..100 events per request
```

Event fields: see the table above; unknown fields must be ignored
(forward-compat). The client treats **any 2xx** as accepted (and clears the
sent events from its spool); anything else means the events are retried in
a later batch, so the endpoint should be idempotent-tolerant of duplicate
events (same anonymousId + ts + command).

Server-side recommendations:

- Rate-limit by IP; reject bodies > 256 KB and batches > 100 events (400).
- Validate `schema == "cli-command/v1"`; drop (don't error) unknown schemas.
- Don't store the source IP with events — the payload is designed to stay
  anonymous.
- Respond 204 with an empty body on success.

### Security model & abuse resistance

The endpoint is **deliberately unauthenticated**: any credential shipped in
a public binary is extractable, so an embedded key or HMAC would be theater,
and requiring user auth would tie events to identities and break the
anonymity promise. This is the standard posture for CLI telemetry
(Homebrew, .NET, Next.js). Consequences and countermeasures:

- **The endpoint is a write-only suggestion box.** It can be lied to, but it
  cannot leak or mutate anything. There must be no read path.
- **No auth context, ever.** The gateway route must not require, forward, or
  log `Authorization` headers or session cookies for `/metrics` — it should
  be impossible for a bearer token to flow into telemetry storage.
- **Poisoning is dampened statistically, not prevented.** Anyone can POST
  fake events; defend at query time, not ingest time:
  - dedup on `(anonymousId, ts, command)`;
  - cap the events counted per `anonymousId` per day (e.g. 2,000) so one
    spammy id can't dominate;
  - prefer "distinct anonymousIds doing X" over raw event counts for any
    metric that matters;
  - treat single-id or single-day spikes as noise until corroborated.
- **Volume abuse** is an edge concern: IP rate limit (e.g. 60 req/min),
  the body/batch caps above, and a WAF/bot rule in front if available.
  Ingest should stay cheap — validate, append, 204; no fan-out work
  attackers can amplify.
- **Duplicates are normal, not an attack**: the client re-sends batches the
  server never 2xx'd, so ingestion must be idempotency-tolerant (the dedup
  key above).
- **Retention**: usage telemetry goes stale fast; a 13-month TTL bounds both
  storage and blast radius.
