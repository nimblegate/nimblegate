> ⚠ **Groups section superseded.** `@`-group bundles (`@tier-1`, …) were removed in
> v0.1.0; the selection primitive is **kits** (see [frames.md](frames.md)). The
> **whitelist** half of this document is still current. Frame IDs use frontmatter
> categories (`commands/`, `web/`…), not directory names (`command-safety/`, `convention/`).

# Groups and whitelist (V0.5)

V0.5 introduced two project-level configuration surfaces that change how you declare *which frames run* and *which specific findings you've vetted*. They compose with the existing `appframes.toml` config and the in-source `appframes:disable` markers; neither replaces the older mechanisms.

## Groups (`@`-prefix bundles)

Groups let you enable a set of related frames as a single line in `appframes.toml`:

```toml
[frames]
enabled = ["@tier-1", "convention/*"]
```

The engine expands `@`-prefix entries at config-load time. Downstream code (and `nimblegate lint`) sees the flat list of frame IDs + wildcards.

### Stdlib groups

Three bundles ship with nimblegate:

| Group              | Members                                                        |
|--------------------|----------------------------------------------------------------|
| `@tier-1`          | All 8 catastrophic-prevention frames (git-safety, fs-safety, command-safety, security) |
| `@tier-6`          | All 3 doc-enforcement frames (convention/*) |
| `@security-strict` | `security/*` + `@tier-1` (recursive composition) |

`nimblegate list --group @tier-1` shows the exact expansion.

### Project-defined groups

Drop `.appframes/groups.toml` to define your own bundles:

```toml
[my-team-strict]
members = [
    "@tier-1",
    "@tier-6",
    "security/no-innerHTML-user-input",
]
```

Member entries can be:
- Exact frame IDs (`security/no-hardcoded-credentials`)
- Category wildcards (`security/*`)
- Other group references (`@tier-1`)

Recursion is allowed up to 16 levels; cycles are detected and reported in the unknown-references list.

### Shadowing stdlib groups

A project group with the same name as a stdlib group **replaces** it entirely. There is no implicit merge: copy the stdlib members you want into your override. Explicit shadow is clearer than append-or-prepend semantics.

`nimblegate lint` surfaces unknown `@`-references as warnings (not fatal, your other entries still take effect):

```
⚠️  appframes.toml references 1 unknown @-group(s):
   - @made-up
   (typo? defined in .appframes/groups.toml? built-in: @tier-1, @tier-6, @security-strict)
```

### CLI workflow

```bash
nimblegate list                          # all loaded frames, tier-sorted
nimblegate list --group @tier-1          # only tier-1 members
nimblegate enable @tier-1                # add to appframes.toml (alphabetized rewrite)
nimblegate disable convention/*          # remove a wildcard line
```

`enable` / `disable` validate the target against the registry + stdlib groups before writing. Typos hard-error with a hint:

```
nimblegate enable: unknown @-group "@made-up" (available: @security-strict, @tier-1, @tier-6)
```

## Whitelist

The whitelist is for vetted exemptions you don't want to bury in source comments: vendored code, generated files, project-wide allowlists. It lives in `.appframes/_canonical/whitelist.toml` as an array of `[[entry]]` tables.

```toml
[[entry]]
frame   = "command-safety/curl-pipe-shell"
path    = "scripts/install/*.sh"
pattern = "myapp.example.com"
reason  = "vetted bootstrap; checksum-pinned upstream"
expires = "2026-09-01"

[[entry]]
frame  = "security/*"
path   = "vendor/**"
reason = "vendored deps; upstream maintains security"
```

### Fields

- `frame` (required): exact ID, `category/*` wildcard, or `*` for all frames
- `path` (required): doublestar-style glob (`**` deep, `*` segment, `?` single char). Defaults to `**` when omitted.
- `pattern` (optional): substring filter against the `Hit.Label`. Useful for "allow THIS specific match, not every curl line in this file."
- `reason` (REQUIRED): audit-grade justification. Load fails if empty.
- `expires` (optional but recommended): `YYYY-MM-DD`. After this date, the entry becomes inactive (gates fire again) and `nimblegate lint` surfaces it as expired.

### Fail-closed semantics

Any load failure surfaces to the user; gates fire normally until the whitelist parses cleanly:

| Condition                            | Behavior                                       |
|--------------------------------------|------------------------------------------------|
| File missing                         | Silent; no exemptions                          |
| Malformed TOML                       | Hard error                                     |
| Entry missing `reason:`              | Hard error                                     |
| Entry has invalid `expires:` format  | Hard error                                     |
| Entry references unknown frame ID    | Hard error (catches typos)                     |
| Entry references unknown category    | Hard error                                     |
| Entry's `expires:` date is past      | Inactive; `nimblegate lint` flags ⚠            |
| Entry never matched a hit            | `nimblegate whitelist list --unused` reports it |

### Pipeline ordering

The suppression pipeline applies in this order:

1. Frame runs → produces raw `CheckResult` with `Hits`
2. In-source markers (`# appframes:disable [-next-line] <frame-id>`) filter hits
3. **Whitelist entries filter hits**
4. Severity overrides applied to surviving hits
5. Cross-frame dedup collapses `(file:line, dedup-key)` groups
6. Render output

The audit log writes the raw (pre-suppression) results first, then a separate `{"kind":"whitelist-suppression",...}` JSONL line per suppressed hit. Silent bypass is impossible.

### Specificity attribution

When multiple entries match the same hit, the most-specific one gets the match-count credit (so `whitelist list --unused` correctly reports the broader entry as unused):

| Specificity score | Shape                       |
|-------------------|-----------------------------|
| +4                | exact `category/name`       |
| +2                | `category/*` wildcard       |
| +0                | `*` (matches every frame)   |
| +1                | adds `pattern` (independent)|

### CLI workflow

```bash
nimblegate whitelist list             # all entries with status (active / expired / unused)
nimblegate whitelist list --expired   # only past-expiry entries
nimblegate whitelist list --unused    # entries that didn't match this run
nimblegate whitelist list --json      # structured shape for scripting / CI
```

`nimblegate lint` also surfaces whitelist health inline:

```
Whitelist (/path/.appframes/_canonical/whitelist.toml):
  3 total - 2 active, 1 expired
  ⚠ expired: frame=command-safety/curl-pipe-shell path=scripts/old/*.sh (expires=2024-12-01) - old bootstrap
  (run `nimblegate whitelist list --unused` after `nimblegate check` for usage hygiene)
```

### When to use which override mechanism

| Mechanism                       | Scope                  | Use when                                      |
|---------------------------------|------------------------|-----------------------------------------------|
| `# appframes:disable[-next-line] <id>` (in source)   | one file or one line | The exemption belongs *with* the code; you own the file |
| Whitelist entry                 | path-glob / pattern   | Vendored / generated / project-wide vetted cases that you've reviewed |
| Scan-ignore (`[scan] exclude-paths` / `.appframes-ignore`) | path / directory | Served content the project distributes as-is (downloads, uploads); nimblegate should never open these in the first place. See [`scan-ignore.md`](scan-ignore.md). |
| `[frames.<cat>.<name>] severity` (in appframes.toml) | the whole frame globally | Demote BLOCK → WARN across the project |
| `nimblegate git --force-yes --reason="..."`           | one shell invocation | git-wrap or command-wrap one-shot bypass with audit |

In-source markers and the whitelist suppress at the same point in the pipeline (after the file was opened and a hit was produced). Scan-ignore operates *earlier*: the file is never opened at all. Pick the one whose scope matches the exemption.

### Common intentional-finding patterns (handle with a whitelist, not a frame change)

nimblegate is deterministic: it flags the codifiable pattern in the **source it scans**, and it can't know your build / template / runtime context. So some findings are *correct yet intentional in your setup*: nimblegate behaved as expected; you simply have context it can't see. The honest resolution is an **audited whitelist entry** (the required `reason` records *why*), **not** disabling the frame, since the frame keeps protecting every file that *isn't* the intentional case. These recur often enough to call out:

| Pattern | Frame(s) | Why it fires (correctly) | Why you whitelist it |
|---|---|---|---|
| **Build-time templated meta** | `convention/html-seo-meta`, `convention/html-required-meta` | The check scans each source page for the meta tags; your shared template / layout (or a build script) injects them into the *built* output, so the **source** page legitimately has none: nimblegate scans source, not the built `index.html`. | The rendered page is correct and the template is the single source of truth. Whitelist the templated page paths. (Templating the meta in a layout is literally this frame's own recommended fix; the per-file regex just can't see across the template.) |
| **Deliberate `curl \| sh` installer** | `command-safety/curl-pipe-shell` | The pattern genuinely pipes a download into a shell. | It's your maintained convenience-installer (the `curl … \| sh` convention). Whitelist with a reason, or, for a *public* installer, harden it (checksum / signature verify) rather than accept it. |
| **Test-fixture secrets** | `security/no-private-keys-in-repo`, `security/no-hardcoded-credentials` | The fixture contains a real-looking key / credential string. | They're fake fixtures that exercise the detector, not live secrets. Whitelist the test paths, and confirm they're unmistakably non-functional. |

Example entries:

```toml
[[entry]]
frame  = "convention/html-seo-meta"
path   = "tools/**/index.html"
reason = "SEO meta injected from the shared navbar/footer template at build; built page has it"

[[entry]]
frame  = "command-safety/curl-pipe-shell"
path   = "cmd/installer/install.sh"
reason = "maintained convenience installer; documented curl|sh install method"

[[entry]]
frame  = "security/no-private-keys-in-repo"
path   = "**/*_test.go"
reason = "test-fixture keys exercising the detector, not real secrets"
```

**Rule of thumb:** if a finding is real-but-intentional *because of context nimblegate can't see* (a build step, a template, a fixture), whitelist it with a `reason`: nimblegate is behaving as designed and the call is yours. If instead nimblegate is *wrong about the pattern itself* (a genuine mis-match), that's a frame bug: fix the frame or open an incident, don't whitelist around it. The two cases look similar but have opposite remedies.
