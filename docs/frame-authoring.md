> ⚠ **Partly out of date.** The authoring workflow below is current, but the
> `category` list is stale: there are **12** frontmatter categories (security,
> network, filesystem, git, commands, app-correctness, database, web, documentation,
> platform, framework, encoding), not 7 directory names. See [frames.md](frames.md).

# Authoring frames

This guide covers writing and editing project-local frames under `.appframes/`.

## Before you write a frame: do you actually need one?

Frames are markdown frontmatter + Go check functions compiled into the binary. Adding one requires: writing the markdown, writing the Go check, registering it in `internal/commands/builtin.go`, opening a PR, deploying the new binary. That's hours-to-days from "I noticed a pattern" to "it's running in production."

If the rule you want is **"warn / block if `<regex>` appears in files matching `<glob>`,"** you don't need a frame. The dashboard's `/policy` page has a **Custom linters** tab where operators author repo-scoped regex rules through a form: no Go, no PR, no deploy. The linter flows through the same engine, audit log, whitelist, severity tuning, and event recording as built-in frames. Adding one takes seconds.

Use a frame when:

- The pattern needs **more than line-by-line regex**: parsing a JSON / TOML / YAML config, walking git history, checking semver constraints, comparing tree state across paths, calling out to git plumbing
- The pattern should ship to **every repo** that enables a kit (the stdlib catalog), not just one repo
- The check needs **stronger trust guarantees**: code-reviewed before merge, immutable at runtime (the dashboard process cannot rewrite a compiled frame, by design; Layer 4 in `docs/server/SECURITY-MODEL.md`)

Use a linter when:

- One specific repo has an in-house anti-pattern other repos don't share
- The author is an operator, not a frame-stdlib maintainer
- The rule can be expressed as glob + regex
- You want preview-before-save against the actual repo HEAD (the linter form has a Preview button; frames don't)

The two categories are designed to compose: frames cover the universal stdlib, linters cover the repo-specific tail. Most "I noticed a pattern in our codebase" cases are linters. Authors land on the Custom linters tab in the dashboard's `/policy` page; the form there has live Preview against the actual repo HEAD so you can iterate on a regex before saving.

## Quick start

```bash
# Validate every frame (stdlib + project) without running any checks
nimblegate lint

# Run frames against the current project
nimblegate check
```

`lint` is the right command to use after editing a frame. It catches parse
errors, missing required fields, and unknown enum values before you ship the
change.

> **Coming from a real incident?** If the frame you're about to author was
> motivated by a specific footgun, capture it first with
> `nimblegate incident new --title "..."` and use
> `nimblegate incident promote <slug> --category ... --name ...` to get a
> pre-filled frame stub that already references the incident note. See
> [`incident-pipeline.md`](incident-pipeline.md) for the loop.

## Frame file layout

A frame is a markdown file with a YAML frontmatter block at the top:

```markdown
---
name: my-custom-rule
category: security
severity: WARN
triggers: [pre-commit, cli]
applies-to:
  files: ["**/*.js"]
---

# My custom rule

Human-readable explanation of what this frame catches and how to fix violations.
```

The file goes under `.appframes/<category>/<name>.md`. The category in the
path is informational; the `category:` field in frontmatter is what actually
determines the frame ID.

### Required fields

- `name`: kebab-case identifier, unique within the category
- `category`: one of: `git-safety`, `fs-safety`, `command-safety`, `network-safety`, `security`, `app-correctness`, `convention`
- `severity`: one of: `BLOCK`, `WARN`, `INFO`
- `triggers`: non-empty list from: `cli`, `pre-commit`, `git-wrap`, `watcher`, `server`

### Optional fields

- `applies-to.files`: globs the check should examine
- `applies-to.commands`: command prefixes the check should intercept
- `canonical-refs`: names of TOML files under `.appframes/_canonical/` the check reads

### V0.5 metadata fields (all optional)

These additions feed the cross-frame dedup pipeline, group bundles, and `nimblegate list / info` UX. Every field has a sensible zero-value default; pre-V0.5 frames continue to load unchanged.

- `tier`: integer 1-6 indicating destructiveness band. 1 = catastrophic (credential leak, history rewrite, filesystem wipe); 6 = cosmetic / doc-enforcement. Defaults to 3 (warn-grade hygiene) when omitted. Drives display ordering in `nimblegate lint` and the default `nimblegate list` sort, and feeds the built-in `@tier-1` / `@tier-6` groups.
- `tags`: free-form list of cross-cutting labels (e.g. `[secrets, supply-chain]`). Used by `nimblegate list --tag <name>` for filtering. No fixed vocabulary.
- `dedup-key`: `"file"` or `"file:line"`. When set AND the frame populates `Hits`, the engine groups hits across frames sharing the same `(scope, dedup-key)` so the user sees ONE row per location instead of N rows for N frames. Omit to opt out of dedup participation.
- `runs-after`: list of frame IDs this frame should be displayed *after*. Best-effort ordering only; frames still run independently. Useful for "this frame's finding makes more sense once the user has seen X."
- `time-cost-hours-prevented`: per-hit time estimate used by [`nimblegate audit analyze`](audit-analyzer.md) when computing the project's prevented-debug-time total. Optional; omit to fall back to the project's `[time-estimates] tier-N` override or the built-in tier default. When `nimblegate incident promote` creates a frame from an incident with `time-cost-hours: 3`, this field is auto-filled to `3`.

```yaml
---
name: no-private-keys-in-repo
category: security
severity: BLOCK
tier: 1
dedup-key: file:line
triggers: [pre-commit, cli]
applies-to:
  files: ["**/*"]
---
```

When a frame produces file:line findings, populate `engine.Hit` entries on the `CheckResult` so the dedup pipeline (and the V0.5 whitelist) can operate on them. Frames that report command-level or project-level findings can skip `Hits`; they pass through the pipeline unchanged.

## Editor integration (live validation)

A JSON Schema for the frontmatter block lives at
`docs/schemas/frame-frontmatter.schema.json`. Pointing your editor's YAML
language server at this schema gives you autocomplete, enum validation, and
inline error highlights as you type.

### VS Code (with the YAML extension)

Add to your project's `.vscode/settings.json`:

```json
{
  "yaml.schemas": {
    "./docs/schemas/frame-frontmatter.schema.json": [".appframes/**/*.md"]
  }
}
```

### Neovim (with yaml-language-server via lspconfig)

In your YAML LSP config, add:

```lua
require'lspconfig'.yamlls.setup{
  settings = {
    yaml = {
      schemas = {
        ["./docs/schemas/frame-frontmatter.schema.json"] = ".appframes/**/*.md"
      }
    }
  }
}
```

### Editors without a YAML LSP

You can still validate by running `nimblegate lint` after each edit. The
schema is just a faster feedback loop; correctness is enforced by the
loader either way.

## Partial-load behaviour

If one frame in `.appframes/` fails to parse, **the rest still load**. The
errors are printed to stderr, but `nimblegate check` continues with the valid
frames. This is intentional: one broken frame should not disable an entire
project's enforcement.

`nimblegate lint` is the dedicated tool for finding broken frames; its exit
code is 1 if any frame fails to parse.

## Override stdlib frames

A project frame with the same `category/name` as a stdlib frame replaces the
stdlib version entirely (triggers, severity, check function, everything).
`nimblegate lint` reports overrides in its output so you can spot them.

If you only want to change severity without replacing the check, use the
`[frames.<category>.<name>]` table in `appframes.toml` instead:

```toml
[frames.security.no-innerHTML-user-input]
severity = "WARN"
```

## Dotfiles are skipped

Files starting with `.` (e.g. `.draft.md`) and hidden subdirectories
(e.g. `.git/`) inside `.appframes/` are skipped by the loader. Use this to
park work-in-progress frames without breaking `nimblegate check`.

## Reading file content: the security pattern

Frames whose Go implementations read file content **must** use the
`ReadFileBounded` helper from `internal/checks/checkcommon.go`, never
raw `os.ReadFile`. The helper caps the read at `DefaultMaxFileBytes`
(1 MiB) so a malicious push containing a multi-GB file can't OOM the
gateway during pre-receive evaluation.

```go
// Canonical pattern:
data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
if !ok {
    continue // file missing, oversized, or unreadable - skip
}
```

This is enforced by `TestNoUnboundedReadInChecks` (runs as part of
`go test ./internal/checks/...`): any new check file that calls
`os.ReadFile` without also calling `info.Size()` or `ReadFileBounded`
fails the test at PR-review time. New frames automatically inherit the
size-cap protection without each author having to remember.

If your frame genuinely needs to scan a larger file (rare; name the
specific shape in the comment), define a per-frame constant and pass
it explicitly:

```go
const myFrameMaxFileBytes = 4 << 20 // 4 MiB - needed for <reason>
data, ok := ReadFileBounded(file, myFrameMaxFileBytes)
```

The audit test still passes because `ReadFileBounded` is the entry
point. Larger caps are operator-visible because they appear in the
frame source.

Why this rule: Go's `regexp` is RE2 (linear-time, no catastrophic
backtracking), so the engine itself can't be ReDoS'd. The remaining
vector is unbounded input size: RE2's O(n) is in bytes, and n is
unbounded if the frame slurps a whole file. The size cap closes that
last hole. See `docs/server/SECURITY-MODEL.md` "Frame execution
limits" for the full threat model.

## Common mistakes

The schema catches most of these; `nimblegate lint` catches all of them:

- **Missing `category`**: `category: <one of the 7>` is required.
- **Tabs instead of spaces in YAML**: YAML disallows tabs for indentation.
- **No opening `---` fence**: the very first line of the file must be exactly `---`.
- **Unclosed frontmatter**: second `---` fence must be present before the body.
- **`triggers: []`**: an empty triggers list is invalid; the frame would never fire.
- **Unknown trigger name**: `triggers: [foo]` will be rejected by the schema and silently ignored at runtime.
- **Raw `os.ReadFile` in the Go implementation**: use `ReadFileBounded` (see "Reading file content" above).
