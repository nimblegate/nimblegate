# Agent stats API: gateway decision analytics for agents

> Reference for operators wiring an AI agent or automation into the gateway's
> decision-log analytics. Covers: what the surface is, how to set it up,
> the seven tools and REST routes, behavior under edge conditions, and
> bearer-token management.

---

## What it is

The agent stats API gives an AI agent (or any HTTP client) read-only access
to the gateway's decision-log analytics, without touching the dashboard.
Numbers are computed by SQL directly over the frame-validated decision log;
the model receives structured data and narrates from it. Every aggregate is
traceable: the `decisions` tool and `/api/v1/decisions` endpoint return the
individual receipts (timestamp, verdict, repo, refs, top findings) that
back any summary, so an agent can show its work.

The surface has two transports: an MCP endpoint for tool-calling agents, and
a REST endpoint for scripts and custom integrations. Both share the same data
and the same bearer-token auth.

---

## Setup

**1. Mint a token.**

```bash
nimblegate gateway token new <label>
```

The token is printed once and stored as a SHA-256 hash in `_auth.db`.
Copy it: it cannot be retrieved again.

**2. Point your MCP client at the gateway.**

Add the gateway as an MCP server using HTTP+JSON-RPC 2.0 (no SSE):

```
URL:    http://<host>:7900/mcp
Header: Authorization: Bearer <token>
```

Most MCP clients accept a headers map in their server config. The gateway
exposes seven tools (see [The tools](#the-tools) below).

**3. Or use REST directly.**

```bash
curl -s -H "Authorization: Bearer nbg_..." \
  "http://127.0.0.1:7900/api/v1/bounce-rate?days=30&min=5"
```

All routes live under `/api/v1/` and accept the same query parameters
as the MCP tool arguments (see [REST reference](#rest-reference)).

---

## The tools

| Tool | What it answers | Notable args |
|---|---|---|
| `gate_stats` | Overall decision counts, accept/reject breakdown, per-repo activity, and the top firing rules for the period | `repo`, `days` |
| `bounce_rate` | Per-repo ratio of rejected pushes to total pushes (which repos push-and-bounce the most) | `repo`, `days`, `min_pushes` (default 5; omits repos below the threshold) |
| `top_rules` | Which frames fired most often, with finding counts | `repo`, `days`, `severity` |
| `recurring_findings` | Findings that keep coming back across multiple pushes from the same repo (candidates for fixing at the source or whitelisting with a reason) | `repo`, `days`, `limit` |
| `decisions` | Individual push receipts: timestamp, verdict, repo, refs, and top findings (the raw evidence behind any aggregate) | `repo`, `days`, `result` (accepted\|rejected), `limit` |
| `time_saved` | Estimated debugging hours the gate prevented: distinct blocking findings × per-frame hours-per-hit (actual = blocked-and-fixed; modeled = conservative upper bound). Same math as the dashboard's Time saved tab | `repo`, `days`, `limit` |
| `what_changed` | Recent commits in a repo (or all repos): what changed, where to look, and the gate's verdict on each pushed tip. Rejected pushes don't advance refs, so the tag is normally "accepted" (with any warnings that rode along) | `repo`, `days`, `path`, `query`, `limit` |

All tools accept:

- `repo` (string, optional): filter to one repo; omitting covers all repos.
- `days` (int, optional, default 30, max 365): lookback window in days.
  Out-of-range values clamp with a note in the response.
- `severity` (string, optional, `top_rules` only): one of `BLOCK`, `ERROR`,
  `WARN`, `INFO`; unknown values are ignored with a note. Other tools accept
  but ignore it.
- `result` (string, optional, `decisions` only): `accepted` or `rejected`.
- `min_pushes` (int, optional, default 5, `bounce_rate` only): minimum push
  count to include a repo; filters out repos with too little activity to
  produce a meaningful rate.
- `limit` (int, optional, default 10, max 50, `recurring_findings`,
  `decisions`, and `time_saved`): row cap on list results (recurring
  findings, decision receipts, or per-frame ROI rows). The top-firing-rules
  list is capped at 20 internally, shared by both `top_rules` and `gate_stats`
  (their "top frames" come from the same query).
- `path` (string, optional, `what_changed` only): limit to commits touching
  this path.
- `query` (string, optional, `what_changed` only): keyword to grep commit
  messages.
- `format` (string, optional, MCP only): `text` (default, narrated lines) or
  `json` (the structured `{data, notes}` envelope as the result text). Pass
  `json` when an agent needs to compute or correlate on the numbers rather
  than narrate them: the model gets structured data instead of prose. (REST
  always returns the JSON envelope, so `format` is a no-op there.)

Tool results are labeled text with a parenthetical period header, e.g.
`(gateway decision log, last 30 days)`, unless `format=json`, which returns
the structured envelope instead.

---

## REST reference

### Routes

| Method | Path | Tool equivalent |
|---|---|---|
| `GET` | `/api/v1/stats` | `gate_stats` |
| `GET` | `/api/v1/bounce-rate` | `bounce_rate` |
| `GET` | `/api/v1/top-rules` | `top_rules` |
| `GET` | `/api/v1/recurring` | `recurring_findings` |
| `GET` | `/api/v1/decisions` | `decisions` |
| `GET` | `/api/v1/time-saved` | `time_saved` |
| `GET` | `/api/v1/changes` | `what_changed` |

Query parameters mirror the tool arguments: `repo`, `days`, `severity`,
`result`, `min` (maps to `min_pushes`), `limit`, `path`, `query`.

### Example

```bash
curl -s \
  -H "Authorization: Bearer nbg_..." \
  "http://127.0.0.1:7900/api/v1/bounce-rate?days=30&min=5"
```

### Response envelope

Every response is a JSON object with two keys:

```json
{
  "data": { ... },
  "notes": ["repo myapp not found - answered for all repos (known: api, frontend)", "days clamped to 365"]
}
```

`data` carries the result payload (structure varies per route).
`notes` is an array of informational strings, omitted when there is
nothing to report. Callers should surface notes alongside results;
they carry information the agent needs to give an accurate answer.

---

## Behavior notes

- **Browser clients (CORS):** both endpoints answer preflight `OPTIONS` and
  send permissive CORS headers, so browser-based MCP clients like the
  llama.cpp webui connect directly. In the webui's header fields use name
  `Authorization`, value `Bearer nbg_...`; the word `Bearer` belongs in the
  value. Safe to allow any origin: auth is the bearer header, never cookies.

**Parameter clamping.** Out-of-range values for `days` (max 365) and `limit`
(max 50) are silently clamped to the allowed maximum. The clamp is reported
in the `notes` array so the caller knows the effective value used.

**Unknown repo recovery.** Requesting a `repo` that the gateway does not
recognise does not return an error. Instead the query runs across all repos
and the `notes` array carries a message of the form:
`"repo X not found - answered for all repos (known: ...)"`.
This lets an agent that misremembers a repo name still get a useful answer.

**Observe-mode banner.** When a per-repo query targets a repo configured
`observe = true`, the `notes` array leads with
`"⚠ OBSERVE MODE - <repo> records findings but NEVER blocks pushes ..."`.
Observe mode is silent to the pushing client by design, so this report-side
banner is the operator's signal that the gate is advisory-only for that repo.
A BLOCK-severity finding was recorded but the push was relayed, not
rejected. The flag is read fresh from the repo's `gateway.toml` per request.
See [What it catches and how it acts](../README.md#what-it-catches-and-how-it-acts).

**Rate limiting.** The agent API enforces 60 requests per minute per token.
Requests over the limit receive a `429 Too Many Requests` response with a
`Retry-After` header indicating how long to wait before retrying.

**Ingest staleness.** The analytics index refreshes at most every 5 seconds.
If a refresh fails (database contention, I/O error), the API continues
serving the last successfully computed data and adds
`"analytics refresh failed - serving existing data"` to the `notes` array.
Stale data is always better than an error; the note tells the caller.

**Excerpt redaction.** Finding excerpt text (the matched line from the
pushed file) is not included in agent API responses. A dashboard flag
`--agent-api-excerpts` exists but defaults to `false` and is reserved for
future receipts enrichment; do not rely on excerpt text being present in
the current release.

**`--auth=off`.** When the gateway is started with `--auth=off`, no token
verifier is wired, so the agent API is disabled and every request returns
`503 Service Unavailable`. (Tokens are SHA-256-hashed in `_auth.db`; with auth
off there is no verifier to check a presented token against; it's the missing
verifier, not a missing file, that triggers the 503.)

---

## Token management

Tokens are managed via the `nimblegate gateway token` subcommand. Flags
`--policy-root` (default `/etc/nimblegate-gateway/repos`) and `--auth-db`
are accepted in any position.

**Mint a new token** (label is a human-readable name for your records):

```bash
nimblegate gateway token new <label>
```

The full token is printed once. It is stored SHA-256-hashed in `_auth.db`
and cannot be retrieved again; store it securely at mint time.

**List active tokens:**

```bash
nimblegate gateway token list
```

Shows token IDs, labels, and creation timestamps. Token values are not shown.

**Revoke a token by ID:**

```bash
nimblegate gateway token revoke <id>
```

Revocation takes effect immediately. The token ID comes from `token list`.
