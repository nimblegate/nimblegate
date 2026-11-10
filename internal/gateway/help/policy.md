# Policy

Per-repo rule selection. The **Editing:** dropdown picks the repo; the tab strip below it splits the page into three focused views. All three tabs share the active-repo selector: switching tabs keeps your repo, switching repos keeps your tab.

> **Observe vs enforce.** This page sets *which* frames run and at what severity, but whether a finding actually **blocks** a push depends on the repo's **mode** (the `off · observe · enforce` badge in the top bar, set when you register the repo). In **observe** mode every finding, even BLOCK-severity, is recorded but the push is relayed anyway: nothing is rejected. Flip the repo to enforce (untick *observe-only* on the repo, or `observe = false` in its `gateway.toml`) for BLOCK frames to actually stop a push.

## Next push preview

For the selected repo, the **Next push preview** section states what the gate would do if the latest pushed code were pushed again under the current policy: "With this policy, the next push would: block X, warn Y", plus an expandable list of findings (severity, frame, file:line, label). It is the gate's own evaluation, not an estimate: the latest commit of the default branch (or, when the gate does not check that branch, the most recently committed gated branch) is extracted and checked with the same frames, gateway-held whitelist, `[scan]` settings, linters, size cap and scan timeout the pre-receive hook uses. The page opens at once and the section fills in when the check is done, also after each frame tick. The dashboard checks every repo in the background every 30 seconds, so after a push or a policy change the result is usually ready before you open the page; results are cached per commit and policy, so an unchanged repo is not rescanned. A repo with no pushed code yet says so instead.

- **Whitelist this**: each BLOCK row offers a one-line reason box. Submitting adds an exact-path entry (that one file, that one frame) to the repo's gateway-held whitelist, the same file the gate reads, and logs a `whitelist-add` event. The reason is required; paths with globs, `..` or line breaks are refused (use the Whitelist tab for patterns). Only on a dashboard started with `--allow-edits`.
- **Apply recommended** and the kit **Apply** buttons open a confirm view first. It shows block and warn counts now and after applying, with findings the change adds marked **new**. Nothing is written until you press **Apply**.
- **Switch to observe**: when the repo enforces and the change would make the next push fail, the confirm view also offers **Apply and switch to observe mode**. It is never pre-selected; pressing it puts the repo in observe mode (logged as a `set-observe` event) before the policy is written, so the next push is recorded instead of rejected while you fix or whitelist the findings.

## Frames

Tick which frames nimblegate runs against pushes to the active repo. A frame is on if and only if its ID appears in `[frames] enabled` in the repo's `appframes.toml`.

- **Apply a kit**: `Core` / `Web app` / `CF Pages` / `CF Workers` / `Security strict` / `Encoding strict` / `Agent shortcuts`. Kits are stackable; the chip row at the top shows live counts. Each Apply button opens the confirm view described above.
- **Tick individual frames**: every kit's frames are visible in the browse tree, grouped by the v2 axes: **Core / Domain / Framework / Platform**. Cloudflare nests Cf Pages and Cf D1 as children; HTML sits under Domain (not Framework, since HTML cross-cuts every web framework). Ticking the same frame anywhere updates every browse path that shows it.
- **New custom kit**: save your own named selection for one-click reuse later. Custom kits appear at the top of the browse tree.
- **Override severity**: per-frame BLOCK ↔ WARN ↔ INFO when the default doesn't fit your context.
- **Unknown entries banner**: when `[frames] enabled` holds entries that match no frame (a typo, a renamed frame, or the removed `@group` syntax), a warning above the page lists them. Each push then records a `policy/unknown-entries` WARN; if no entry matches anything, the policy enables no checks and every push to a gated ref is rejected until it names a frame. For `@group` entries, `nimblegate lint` shows the kit to apply.
- **Currently enabled frames**: collapsed summary at the top of the page; click to expand the list grouped per axis. The number in parentheses is the unique-enabled count across all kits and individual ticks.

## Frame vs custom linter: which to use

| Aspect | Frame (stdlib) | Custom linter (UI-authored) |
|--------|----------------|------------------------------|
| Lives at | `internal/stdlib/frames/*.md` + Go check fn, compiled into the binary | `<policy-root>/<repo>/` config, gateway-managed |
| How to add | Markdown + Go + PR + deploy (hours-to-days) | Fill the form on this tab and submit (seconds) |
| Can express | Anything Go can do: parse JSON, walk git history, check semver, run helpers | Glob + regex line-match, single file at a time |
| Scope | Stdlib: every repo that enables it via kit or individual tick | This repo only, invisible to others |
| Trust model | Code-reviewed before merge, immutable at runtime, deploy via passphrase-gated admin SSH | Operator-authored, dashboard can edit, gated by dashboard auth |
| Triggers | pre-commit, cli, git-wrap, pre-receive (per frontmatter) | Pre-receive on the gateway only |
| Severity | Frontmatter default, operator-tunable | Form default, operator-tunable |
| Audit + whitelist | Same path | Same path |
| Disable markers | `appframes:disable <id>` in source / commit msg | Not supported today |
| Preview before saving | No (you wrote the code) | Yes: runs against the materialized HEAD |
| Versioning | git log on the nimblegate repo | What's in policy right now |

**Rule of thumb:** if the check is "warn if `<regex>` appears in any `<glob>` file," it's a linter. If it needs more than line-by-line regex (parsing a config file, walking tree state, calling out to git), it's a frame and goes through the PR path. The PR friction is the trust-model feature: frames are immutable at runtime so the dashboard process can't rewrite the policy it enforces.

To author a linter, switch to the **Custom linters** tab. Frames tab manages frames; Linters tab manages linters; the comparison above helps you pick the right tab before you start.

## Custom linters

Repo-scoped regex rules authored from the dashboard, with no Go code, no rebuild. Useful for codebase-specific patterns that aren't in the stdlib catalog (in-house anti-patterns, deprecated SDK calls, house-style violations).

The form has a **Start from a pattern** dropdown above the inputs: a small library of vetted starter regexes (URLs in source, TODO markers, AWS / Stripe / GitHub / OpenAI key shapes, JWT shape, localhost references, debug-log leaks, hardcoded IPv4). Picking a starter auto-fills name + globs + regex + severity; refine the regex with your agent (paste it into Claude Code / Cursor and ask "this catches X, can you tighten it to also catch Y") then come back and **Preview** to see what it would have flagged on the latest push. Starters are starting points, not finished checks: operators are expected to tighten globs and regex per repo before saving.

The form takes:

- **Name**: lowercase letters / digits / dashes, e.g. `internal-secret`.
- **File patterns**: comma-separated globs, e.g. `*.go, src/**/*.ts`.
- **Regex**: the pattern that flags a finding.
- **Severity**: `WARN` (default), `INFO`, or `BLOCK`.

Two buttons:

- **Preview**: runs the regex against the latest push so you can see what it would have flagged before saving.
- **Add check**: persists the rule into the repo's policy.

Each existing custom linter shows its name + patterns + regex + severity dropdown + an on/off toggle + delete. Read-only entries (subprocess / built-in linters) show a severity tag instead of editable controls.

## Whitelist

The per-repo silenced findings: `file:line` + frame ID + reason. Survives frame disables (separate list, not a frame setting). New entries land here from the Whitelist button on Stats → Recurring findings.

- Exact paths match one file. Globs (`*` / `**`) match many; the path-input scope hint warns when your pattern matches more than one file.
- **Remove**: deletes the entry; the next push that hits this file:line will re-fire the frame.
- Notification rail editing used to live on this page; it moved to [Auto-PR · Setup](/auto-pr/config) so it can grow without crowding policy.

## Time-prevented estimates

Per-tier hours-per-hit values that weight the [Stats](/stats) "time saved" estimate. One number box per frame tier (1-6); a tier left at its built-in value is marked **default**, an edited one **override**. These are a reporting model only - they do **not** affect gating or relay. Set them per repo if your team's real debugging costs differ from the conservative defaults: Tier 1 = 4h, Tier 2 = 2h, Tier 3 = 0.5h, Tier 4 = 0.25h, Tiers 5 and 6 = 0.1h. Saving writes the `[time-estimates]` section into the repo's `appframes.toml`.

## Common gotchas

- Toggles take effect on the **next push** to this repo; already-in-flight pushes use the policy that was loaded when the push started.
- Disabling a frame doesn't remove its whitelist entries; they sit dormant until you re-enable it.
- Custom linter names must be unique per repo; duplicate names rejected.

For depth: [Frames and kits](https://nimblegate.com/docs/frames-guide) · [Frame reference](https://nimblegate.com/docs/frames/).
