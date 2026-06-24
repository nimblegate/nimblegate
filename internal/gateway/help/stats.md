# Stats

Per-repo view of what was prevented. The headline numbers are the **estimated debugging time saved** by catching issues at push time. The tab strip splits this into two focused views; the time-window dropdown and repo filter at the top apply to both.

## Time window

Pick from the dropdown at the top:

- **All time** (default)
- **Last 24h**
- **Last 7 days**
- **Last 30 days**

## Time saved (default tab)

Two figures per repo:

- **Actually prevented**: only pushes that were blocked AND subsequently fixed (audit-log verified). The conservative, evidence-based figure.
- **Modeled / would-have**: every distinct block multiplied by a per-tier hours-per-hit estimate. The upper bound.

The **Per-frame breakdown** disclosure expands a table with rejected / observed counts + hours-per-hit + source + actual h + modeled h per frame. The `source` column shows whether the per-hit hours came from a per-repo `[time-estimates]` override or the stdlib default.

## Recurring findings

The same `file:line` caught more than once, with a **Whitelist** button per row. Each row shows severity + frame ID + location + seen count + last-seen timestamp.

The Whitelist button opens an inline form:

- **Path**: pre-filled from the finding's `file:line`. A scope hint warns if your pattern matches more than one file.
- **Reason**: why this isn't a real finding. Required.

Below the recurring table sits the **Whitelist** panel: every currently-silenced entry on this repo with frame + path + reason. **Remove** deletes the entry; the next push that hits the same line will re-fire the frame.

## Common gotchas

- Brand-new repos with few pushes show very small actual numbers; the metric only counts blocked-then-fixed pairs.
- Per-frame hours-per-hit can be overridden per-repo; the per-frame breakdown's `source` column shows where the value came from.
- The Whitelist panel here is the same data as [Policy → Whitelist](/policy?tab=whitelist); editing in either view shows up in both.

For depth: [docs/audit-analyzer.md](https://github.com/nimblegate/nimblegate/blob/main/docs/audit-analyzer.md).
