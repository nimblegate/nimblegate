# Audit log analyzer

`nimblegate audit analyze` runs retrospective pattern detection over `.appframes/audit.log` (plus rotated siblings) and surfaces three classes of finding, alongside an estimate of debug time prevented.

It is pure-local, mechanical (counts + multiplications), and emits no telemetry. Every number in the output is reproducible from the audit log + frame frontmatter + project config.

## What it surfaces

### 1. Top bypassed frames

Frames with the most `--force-yes` overrides in the window. Output groups by frame ID with the count and the clustered reason tokens:

```
Top bypassed frames (>= 2 bypasses):
  git-wrap/override   3×  reasons: vendor(3) test(2)
    → consider: whitelist entry path="vendor/**" reason="vendored deps"
```

The suggestion is mechanical: when the top hotspot token matches a known pattern (`vendor`, `test`, `ci`, `fixture`, `generated`, …), the analyzer prints a concrete remediation. Otherwise it surfaces the pattern and leaves the call to you.

### 2. Reason-text hotspots

Token frequency across bypass `--reason` strings. The tokenizer:
- Lowercases everything
- Splits on non-alphanumeric/non-hyphen characters (kebab-case identifiers stay intact)
- Strips a leading `--force-yes:` prefix (boilerplate that would otherwise dominate every cluster)
- Filters tokens shorter than 4 chars and a small stopword list (`the`, `and`, `with`, `that`, `reason`, …)
- De-duplicates per reason so one verbose bypass can't dominate

Tokens appearing in ≥ `--min-bypass` (default 2) distinct reasons surface as hotspots. Top 5 per frame.

### 3. Stale frames

Frames present in `appframes.toml` `[frames] enabled` (after group expansion) but with zero audit entries in the window:

```
Stale frames (enabled, zero hits in 30d):
  convention/markdown-link-check-internal  (tier 6)
  → caveat: audit log only reflects this window; older history may show different signal.
```

Wildcards (`security/*`) and groups (`@tier-1`) are skipped: only specific frame IDs are evaluated.

## Time-prevented stats

Alongside the patterns, the analyzer estimates how much debug time the project has been saved by frames firing in the window. Every non-bypass non-PASS / non-SKIP result for a loaded frame contributes `frame.hours-per-hit × 1`. Bypass entries do NOT count: they're the opposite of prevention.

### Resolution precedence

Each frame's per-hit estimate is resolved with this precedence:

1. The frame's own `time-cost-hours-prevented` frontmatter field (most specific).
2. The project's `[time-estimates] tier-N` override for the frame's tier.
3. The built-in `DefaultTimeCostHoursPreventedByTier` table.

Sample output (default human-readable):

```
Estimated time prevented (window):
  Total: 17.0h
    Tier 1 (3 frames hit): 16.0h
    Tier 3 (1 frames hit): 1.0h
  (each frame's per-hit estimate comes from its frontmatter, the project's
   [time-estimates] section, or the built-in tier default - `nimblegate info <id>`)
```

The status line teaser (in `nimblegate status`) shows just the aggregate when > 0h:

```
Estimated time prevented (last 7d): 4.5h  (run `nimblegate audit analyze` for breakdown)
```

### Built-in tier defaults

Conservative by design: the defensible number matters more than the marketing-friendly one:

| Tier | Default | Rationale |
|------|---------|-----------|
| 1 | 4h | Catastrophic prevention (credential leak, force-push to main, fs wipe) |
| 2 | 2h | Application security (XSS, injection class) |
| 3 | 0.5h | Code hygiene |
| 4-5 | 0.25h / 0.1h | Minor compliance |
| 6 | 0.1h | Doc enforcement |

### Plugging in your own historical data

If your previous-project data is more accurate than the defaults, override in `appframes.toml`:

```toml
[time-estimates]
tier-1 = 8     # your team has seen catastrophic incidents cost ~8h each
tier-2 = 3
tier-3 = 0.75
tier-6 = 0.05  # cosmetic frames almost never matter for you
```

Any tier you leave out falls back to the built-in default. Negative values fail config validation.

### Promote auto-fill

When you turn an incident into a frame via `nimblegate incident promote`, the resulting frame stub's `time-cost-hours-prevented` is auto-filled from the incident's `time-cost-hours`. Your own lived experience seeds the estimate. You can edit it later in the frame's frontmatter.

## Honesty rules

Three rules keep the prevented-hours number defensible:

1. **Math is reproducible**: every total is `frame.hours-per-hit × audit-log block-count`. `nimblegate audit analyze --frame <id>` shows which source applied for that frame.
2. **Defaults stay conservative**: built-ins err small. Better to undersell than to overclaim.
3. **Bypasses don't count**: they're recorded but subtract conceptually from "actually prevented" (the user got past the gate).

## CLI

```bash
nimblegate audit analyze                      # report against last 30d
nimblegate audit analyze --window 7d          # narrower window (7d, 24h, etc.)
nimblegate audit analyze --frame <id>         # focus on one frame
nimblegate audit analyze --min-bypass 5       # raise the top-bypassed threshold
nimblegate audit analyze --json               # structured for scripting / future UI
```

Window syntax accepts the same `Nd` / `Nh` / `Nm` forms as `nimblegate status --since`.

## JSON shape

```json
{
  "window": { "start": "...", "end": "...", "days": 30 },
  "entries_analyzed": 247,
  "total_hours_prevented": 18.5,
  "hours_prevented_by_tier": { "tier-1": 16.0, "tier-6": 2.5 },
  "frames_hit_by_tier":      { "tier-1": 3, "tier-6": 2 },
  "top_bypassed": [
    {
      "frame_id": "git-safety/no-force-push-main",
      "bypass_count": 6,
      "reasons":  ["..."],
      "hotspots": [ { "token": "vendor", "count": 4 } ],
      "hours_per_hit": 4.0,
      "hours_source":  "tier-default"
    }
  ],
  "stale_frames": [ { "frame_id": "convention/markdown-link-check-internal", "tier": 6 } ]
}
```

`hours_source` is one of `frame` (frontmatter), `project-tier` (`[time-estimates]`), or `tier-default` (built-in).

## What it does NOT do

Scope boundaries (deliberate):

- **No prediction.** Purely retrospective; no scoring of "this frame will fail soon."
- **No auto-actioning.** Outputs suggestions, never edits `appframes.toml` or the whitelist.
- **No external data.** Doesn't touch git log, shell history, or CI. Only the audit log.
- **No co-occurring-block detection / time-decay weighting**: V2 candidates that need more audit data to be useful.
