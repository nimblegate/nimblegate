# Frames: what they are and how to select them

> Public reference for operators picking up nimblegate for the first time.
> Covers: what a frame is, the canonical taxonomy, how to select frames in
> your project, starter kits, custom user kits, and how to migrate legacy
> configs.

Related docs:

- [`frame-catalog.md`](frame-catalog.md): per-frame list with categories and tiers
- [`frame-authoring.md`](frame-authoring.md): how to write a new frame
- [`frame-patterns.md`](frame-patterns.md): the shared pattern catalog frames build on

---

## What a frame is

A **frame** is a single named check that nimblegate runs at scan time. When
the check matches something in the pushed code, the gateway emits a
**finding**, a structured record naming the frame, the file and line, and a
one-sentence message. What the gateway *does* with that finding (block the
push, allow and record it, or allow it silently) is decided by the finding's
severity, which the operator can tune per-repo on the Policy page.

Frames are gated by project config: a frame is active if and only if its ID
appears in the `[frames] enabled` list in `appframes.toml`. The
dashboard and CLI let you select frames individually, or apply a starter kit
that pre-ticks a curated set.

Each frame ships as a single markdown file. The frontmatter declares the
metadata; the body is prose explaining what the frame catches, why, and how to
fix the kind of finding it produces.

---

## Frame anatomy

The frontmatter fields for a stdlib frame:

```yaml
name: no-hardcoded-credentials      # unique short ID within its category
category: security                  # primary taxonomy category (one of 12 canonical)
subcategory: credentials            # bucket within the category
platform: []                        # optional - cross-lists under Platform > <value>
framework: []                       # optional - cross-lists under Framework > <value>
severity: BLOCK                     # BLOCK | WARN | INFO (operator-tunable)
tier: 1                             # 1–6; used for time-prevented hours math only
tags: [secrets, content-scan]       # free-form search/filter chips
triggers: [pre-commit, cli]         # where the frame fires
applies-to:
  files: ["**/*"]
```

The markdown body follows the frontmatter and explains the frame in plain
English.

Frame files live at `internal/stdlib/frames/<legacy-dir>/<name>.md`. The
directory reflects the historical organisation; the `category:` field in
frontmatter is the logical placement used by the dashboard browse tree and
taxonomy validation.

---

## Canonical category list

Every frame declares exactly one `category`. The loader validates it against
this set. Dashboard hides categories with zero frames (Framework is hidden in
v0.1.0).

| # | Category | What it covers | v0.1.0 subcategories |
|---|---|---|---|
| 1 | `security` | Credentials, XSS / content safety, security headers, transport, invisible-payload attacks (Trojan Source / tag-char / zero-width / homoglyph) | `credentials`, `content-safety`, `headers`, `transport`, `invisible-payload`, `identifier-confusable` |
| 2 | `network` | Proxy / reverse-proxy configs, routing, DNS resolution | `proxy-config`, `routing` |
| 3 | `filesystem` | Destructive paths, mount points, file ops | `destructive-paths` |
| 4 | `git` | Branch discipline, history integrity, push-gate integrity | `branch-discipline`, `history-integrity`, `gate-integrity` |
| 5 | `commands` | Shell safety, trusted execution, package management | `trusted-execution`, `package-management` |
| 6 | `app-correctness` | Env config, data fetching, module loading, routing | `env-config`, `data-fetching`, `module-loading`, `routing` |
| 7 | `database` | Migrations, schema drift | `migrations`, `schema-drift` |
| 8 | `web` | HTML, SEO, a11y, markup validity | `html`, `seo`, `a11y`, `markup-validity` |
| 9 | `documentation` | Markdown, doc drift, TODO discipline, branch consistency | `markdown`, `doc-drift`, `todo-discipline`, `branch-consistency` |
| 10 | `encoding` | BOM, smart quotes, YAML tabs, line endings, mixed indent, en-dash flags, non-printable controls, zero-width in prose | `byte-order-mark`, `smart-quotes`, `yaml`, `line-endings`, `indent`, `dash-substitution`, `control-chars`, `invisible-chars` |
| 11 | `platform` | Hosting/runtime platforms (Cloudflare, CF Pages) | `cloudflare`, `cf-pages` |
| 12 | `framework` | Application frameworks: **reserved, empty in v0.1.0** | none |

Subcategory is free-form within a category. New frames may introduce new
subcategories without prior registration; the loader only requires the field
is non-empty.

---

## Cross-listing via `platform:` and `framework:`

A frame can declare `platform:` and/or `framework:` arrays. When it does, the
dashboard browse tree shows that frame under:

1. Its primary `category > subcategory` location
2. For each value in `platform:`: under `Platform > <value>`
3. For each value in `framework:`: under `Framework > <value>`

All paths share a single tick state: toggling a checkbox anywhere updates every
browse path that contains the same frame simultaneously. There is no separate
"Platform copy" of a frame; it is the same frame ID appearing in multiple places.

**Example:** `security/cf-pages-headers-baseline` has `category: security`,
`subcategory: headers`, `platform: [cloudflare, cf-pages]`. It appears under
Security > Headers, Platform > Cloudflare, and Platform > CF Pages. Ticking it
once from any location enables it.

---

## Selecting frames in your project

Frames are selected by listing their IDs in `appframes.toml`:

```toml
[frames]
enabled = [
    "git-safety/folder-branch-lock",
    "security/no-hardcoded-credentials",
    "security/no-private-keys-in-repo",
    # ...
]

[ui]
applied_kits = ["core"]
```

Rules for the `enabled` list:

- **Flat IDs only.** Each entry is `<dir>/<name>` exactly as shown in the
  frame catalog.
- **No `@`-prefixes.** The `@group-name` syntax is removed in v0.1.0. See
  the [migration section](#migration-from-prefixed-group-configs) below.
- **No wildcards.** `security/*` is not valid. Tick frames individually or
  apply a kit.

A frame is active if and only if its ID appears in this list. The engine reads
it as-is: no expansion, no inheritance.

---

## Starter kits

A **starter kit** is a named, pre-defined set of frame IDs. Applying a kit
writes each of its frame IDs into `[frames] enabled` and records the kit name
in `applied_kits`. Clearing a kit removes its frame IDs from `enabled` and its
name from `applied_kits`. Kits are transparent: you can see exactly which
frames they contain.

Stdlib ships six starter kits:

| Kit | Use case | Frame count |
|---|---|---|
| `core` | Universal floor: every project's baseline. Default on `nimblegate setup`. | 15 |
| `web-app` | Projects shipping HTML pages (any backend). Includes `core`. | 27 |
| `cf-pages-project` | SvelteKit / Astro / Next on CF Pages with D1. Includes `web-app`. | 29 |
| `cf-workers-project` | Cloudflare Workers + Tunnels + Access, no HTML. Includes `core`. | 20 |
| `security-strict` | Adds every `security/*` frame on top of whatever else is applied (includes Trojan Source / tag-char / zero-width / homoglyph attack-class). Stackable with any other kit. | 9 |
| `encoding-strict` | Adds every `encoding/*` frame: BOM, smart quotes, YAML tabs, line endings, mixed indent, en-dash flags, non-printable controls, ZWSP in docs. Stackable with any other kit. | 8 |

Kit CLI:

```
nimblegate kits list
nimblegate kits apply web-app
nimblegate kits clear web-app
```

`nimblegate setup` uses `core` by default. Pass `--kit <name>` to apply a
different kit on setup, or `--kit none` to start with an empty `enabled` list.

---

## Custom user kits

Your project can define its own named kits in `[[ui.user_kits]]` blocks:

```toml
[[ui.user_kits]]
name = "MVP launch gate"
frames = [
    "security/no-hardcoded-credentials",
    "git-safety/folder-branch-lock",
]
```

Custom kits appear in the dashboard browse tree alongside the canonical
categories, and as chips in the applied row.

CLI:

```
nimblegate kits create "MVP launch gate" --frames security/no-hardcoded-credentials,git-safety/folder-branch-lock
nimblegate kits delete "MVP launch gate"
```

`kits delete` removes the kit entry from `[[ui.user_kits]]` but does **not**
untick the frames it contained. If you want the frames unticked too, use
`kits clear` before deleting, or tick them off individually in the dashboard.

---

## Tier metadata (sidenote)

Each frame has a `tier: 1–6` field used only for the time-prevented hours
estimate on the `/stats` page:

| Tier | Modeled hours per finding |
|---|---|
| 1 | 4 h |
| 2 | 2 h |
| 3 | 0.5 h |
| 4 | 0.25 h |
| 5 | 0.1 h |
| 6 | 0.1 h |

Tier is severity metadata, not a selection group. It never appears as a
category, kit name, or selection primitive in any operator-facing surface.

Per-tier hour values can be overridden on the Policy page → **Time-prevented
estimates** if the stdlib defaults don't fit your cost model.

---

## Migration from `@`-prefixed group configs

The `@group-name` config syntax is removed in v0.1.0. Any config with
`@`-prefixed entries or category wildcards in `enabled` will be rejected on
load with an error message listing the equivalent kit.

Migration table:

```
@tier-1          → nimblegate kits apply core
@tier-6          → tick Documentation category frames in dashboard
@web             → nimblegate kits apply web-app
@cloudflare      → nimblegate kits apply cf-workers-project
@cf-pages        → nimblegate kits apply cf-pages-project
@migrations      → tick Database > Migrations frames in dashboard
@security-strict → nimblegate kits apply security-strict
security/*       → tick frames individually in dashboard
```

After updating your config, re-run to verify:

```
nimblegate kits list
nimblegate frames list
```

---

## Severity: what happens at the gate

Three severity values; one per finding. The operator can override a frame's
default severity per-repo from the Policy page.

| Severity | Gate behaviour |
|---|---|
| `BLOCK` | Push rejected synchronously; never reaches upstream. |
| `WARN` | Push allowed and relayed; finding recorded in audit log + dashboard feed. |
| `INFO` | Push allowed; finding recorded, shown less prominently. |

A useful rollout pattern: enable a new frame at `WARN` first, watch how often
it fires on real pushes for a week, then upgrade to `BLOCK` once you've
confirmed the false-positive rate is acceptable.

---

## Triggers: where frames fire

| Trigger | What runs it |
|---|---|
| `pre-commit` | Local `nimblegate` install on the dev box |
| `cli` | Explicit `nimblegate check` invocation |
| `pre-receive` | The gateway on push arrival |
| `post-receive` | The gateway after the gate accepts |
| `git-wrap` | The dev box's git wrapper when commands run |

Most stdlib frames declare `[pre-commit, cli]`. The gateway runs them via the
`pre-receive` path regardless of the declared triggers.

---

## Lifecycle

| Value | Meaning |
|---|---|
| `active` | In the catalog; runs when enabled. |
| `proposed` | Drafted but not ready for general use. Not in any stdlib kit by default. |
| `deprecated` | Superseded; kept readable for the replacement chain. |
| `archived` | Premise no longer applies; removed from active use. |

The dashboard `/frames` page shows `active` only by default.

---

## Patterns: the shared concept catalog

Some failure shapes recur across multiple frames. The
`internal/stdlib/frames/patterns/` directory documents these *patterns*:
abstract failure shapes that frames reference via the `pattern:` frontmatter
field. Examples:

- `quota-window-mismatch`: API rejects silently when the request range
  exceeds an implicit per-dataset cap.
- `secret-in-source`: credential committed to a repo where it can be
  exfiltrated.
- `ambiguous-config-value`: config value that allows multiple valid
  interpretations.

Patterns aren't checks themselves; they're shared vocabulary the catalog uses
to talk about failure shapes consistently. See
[`frame-patterns.md`](frame-patterns.md) for the full reference.

---

## Authoring new frames

Frame files live at `internal/stdlib/frames/<dir>/<name>.md`. The frontmatter
fields are documented in the anatomy section above. For a step-by-step guide
to writing and testing a new frame, see [`frame-authoring.md`](frame-authoring.md).

---

## Where to go next

- Full per-frame list: [`frame-catalog.md`](frame-catalog.md)
- Write a new frame: [`frame-authoring.md`](frame-authoring.md)
- Look at a frame's source: `internal/stdlib/frames/<dir>/<name>.md`
- Live catalog with tick state: dashboard `/frames` page
