# Incident pipeline

`nimblegate incident` closes the loop between **"we got burned"** and **"a frame exists that would have caught it."** It is built around two principles:

1. **Capture moment = footgun moment.** The cheapest time to record an incident is the moment you bypass a frame with `--force-yes`. The CLI prompts you right there.
2. **No new habits required.** A passive nudge in `nimblegate status` catches anything captured later or skipped at the prompt.

The pipeline is mechanical: no AI, no judgment, no remote services. Incidents are plain markdown files under `.appframes/_incidents/` that you version-control alongside the frames they spawn.

## Commands

```bash
nimblegate incident new --title "..."              # scaffold a draft
nimblegate incident list [--status draft|promoted] # browse drafts
nimblegate incident promote <slug> --category --name --tier --severity --triggers
```

### `incident new`

Scaffolds `.appframes/_incidents/YYYY-MM-DD-slug.md` from the embedded template and prints the path.

Flags:

| Flag | Purpose |
|------|---------|
| `--title` (required) | Human-readable title. Slug is derived automatically. |
| `--time-cost-hours` | Estimated debug time (informational). |
| `--tags` | Comma-separated free-form labels. |
| `--from-frame` | Frame ID that was bypassed. Sets `source: bypass`. |
| `--from-reason` | The `--force-yes` reason text. |
| `--from-command` | The command that was bypassed. |
| `--json` | Emit JSON describing the created file (for scripting). |

Setting any of `--from-frame` / `--from-reason` / `--from-command` flips `source: bypass` and pre-populates a context blockquote at the top of the body.

### `incident list`

Reads every `*.md` file under `.appframes/_incidents/` and renders a table with date, status, slug, title, and (when promoted) the frame ID it spawned.

- `--status draft` / `--status promoted` filter to one phase.
- `--json` emits the structured shape consumed by future UI / CI.

### `incident promote`

Reads the named incident, validates the target frame metadata, writes a frame stub at `.appframes/<category>/<name>.md`, and flips the incident's frontmatter to `status: promoted`, `promoted-to: <frame-id>`.

Flags (all required except `--json`):

| Flag | Allowed values |
|------|----------------|
| `--category` | `git-safety`, `fs-safety`, `command-safety`, `network-safety`, `security`, `app-correctness`, `convention` |
| `--name` | kebab-case identifier (matches `[a-zA-Z0-9][a-zA-Z0-9_-]*`) |
| `--tier` | 1 (catastrophic) … 6 (cosmetic) |
| `--severity` | `BLOCK` / `WARN` / `INFO` |
| `--triggers` | comma-separated subset of `cli`, `pre-commit`, `git-wrap`, `watcher`, `server` |

The slug positional can come before or after the flags: `nimblegate incident promote <slug> --category ...` and `nimblegate incident promote --category ... <slug>` both work.

The frame stub frontmatter is fully formed and passes `nimblegate lint`. The body is intentionally a checklist of remaining work (implement the check function, bind in `internal/commands/builtin.go`, add tests, enable, lint clean). The hard part, the check function itself, is what you write.

## Trigger surfaces

Capture friction is the whole game. The pipeline has two surfaces wired into existing CLI flows so you never need to remember a new command.

### 1. Post-bypass prompt

After every `nimblegate git --force-yes ...` or `nimblegate cmd --force-yes ...`, the override is recorded to the audit log and you're asked:

```
nimblegate: --force-yes override recorded (reason: "...")
nimblegate: capture this bypass as an incident? [y/N]
```

Answering `y` scaffolds an incident with `source: bypass`, the reason, the command, and the wrap context already pre-filled. The bypassed-frame reference and the reason text appear as a blockquote at the top of the body.

**The prompt is silent (zero output) when:**

- stdin is not a TTY: protects CI, pipelines, and scripted invocations from blocking on a question no one will answer.
- `APPFRAMES_NO_INCIDENT_PROMPT` is set in the environment: permanent opt-out.
- `APPFRAMES_INCIDENT_PROMPT=off` is set: per-invocation opt-out.

There is no way to silently bypass without leaving an audit entry; the override itself is always recorded before the prompt is considered.

### 2. Status nudge

`nimblegate status` ends with a one-line reminder when uncaptured bypasses accumulate:

```
⚠  3 bypass(es) in last 7d not yet captured as incidents
   capture with: `nimblegate incident new --title "..." --from-frame <id> --from-reason "..."`
```

The check is mechanical: count `override=true` audit entries in the last 7 days, subtract the number of `source: bypass` incident files dated within the same window. The window is fixed (not tied to `--since`) so narrowing the filter doesn't hide the backlog.

The nudge is silent when bypasses ≤ captures: once you've recorded as many incidents as bypasses, the noise stops.

## File layout

```
.appframes/
├── _incidents/
│   ├── 2026-05-18-wrangler-wrong-env.md      # draft
│   └── 2026-05-19-localhost-ipv6-trap.md     # promoted
├── command-safety/
│   └── wrangler-explicit-env.md              # promoted from the incident above
└── ...
```

Underscore-prefixed subdirs (`_incidents/`, `_canonical/`) are nimblegate-managed metadata and are **skipped by the frames loader**. They will not surface as "missing name" errors in `nimblegate lint`.

## Template shape

The embedded template matches the catalog format that incident catalogs in the wild already use (Incident / Detection signal / Frame proposal / Where the check belongs / Generalizes to). Porting entries from an existing catalog is copy-and-paste.

```
---
title: <derived from --title>
date: <YYYY-MM-DD, UTC>
time-cost-hours: <number>
status: draft
source: manual | bypass
source-frame: <frame-id, when source=bypass>
source-reason: <text, when source=bypass>
source-command: <text, when source=bypass>
tags: [...]
---

# <title>

## Incident
What broke, time cost, cross-references.

## Detection signal
What would have flagged this before damage. Concrete signals, not vibes.

## Frame proposal
Candidate frame: ID, severity, tier, triggers, mechanical check.

## Where the check belongs
pre-commit / pre-deploy / runbook / CI gate.

## Generalizes to
Broader pattern this points to.

## Notes
Anything else.
```

After `promote`, the frontmatter gains `status: promoted` and `promoted-to: <category>/<name>`; the body is left intact so the captured context survives.

## What this pipeline is NOT

Hard scope boundary: these are explicitly out of scope to keep the surface narrow:

- **No dashboards, search, or analytics inside the CLI.** Incident files are human notes + frame-seeds. If you need to mine across them, use `grep` / `rg`.
- **No remote sync.** Incidents are git-tracked like everything else.
- **No auto-promotion.** The check function is the hard part; nothing else is worth promoting without it.
- **No pattern detection on the audit log** (yet): repeated-bypass clustering, co-occurring-block detection, and stale-frame surfacing are natural next features but live in a future `nimblegate audit analyze` command, not here.

## Workflow example

```bash
# You hit a footgun in the wrong-env migration class.
$ nimblegate git --force-yes --reason="ran wrong migration, fixed by re-running with --remote" push origin main
nimblegate: --force-yes override recorded (reason: "ran wrong migration, fixed by re-running with --remote")
nimblegate: capture this bypass as an incident? [y/N] y
nimblegate: captured at /repo/.appframes/_incidents/2026-05-18-push-origin-main.md
  next: edit it, then `nimblegate incident promote push-origin-main --category ...`

# Open the file, fill in Incident / Detection signal / Frame proposal,
# rename the title in frontmatter to something descriptive.

$ vim .appframes/_incidents/2026-05-18-push-origin-main.md

# Once you've decided what the gate should be, promote:
$ nimblegate incident promote push-origin-main \
    --category command-safety \
    --name wrangler-explicit-env \
    --tier 1 \
    --severity BLOCK \
    --triggers pre-commit,cli
Promoted incident "push-origin-main" → frame command-safety/wrangler-explicit-env
  frame stub:    /repo/.appframes/command-safety/wrangler-explicit-env.md
  incident file: /repo/.appframes/_incidents/2026-05-18-push-origin-main.md (marked promoted)

# Now the only thing left is writing the check function. The stub body
# tells you exactly what to do.

$ nimblegate lint                              # validates the stub
$ nimblegate enable command-safety/wrangler-explicit-env
```

## See also

- [`frame-authoring.md`](frame-authoring.md): the underlying frame format that `promote` writes against
- [`groups-and-whitelist.md`](groups-and-whitelist.md): exemption + grouping surface for when a frame fires too often
