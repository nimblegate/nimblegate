---
name: approved-registries-only
category: commands
subcategory: package-management
platform: []
framework: []
severity: WARN
tier: 2
dedup-key: file:line
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/pom.xml"
    - "**/build.gradle"
    - "**/build.gradle.kts"
    - "**/settings.gradle"
    - "**/settings.gradle.kts"
    - "**/.npmrc"
    - "**/.yarnrc.yml"
    - "**/requirements*.txt"
    - "**/pip.conf"
    - "**/pip.ini"
pattern: unapproved-dependency-source
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 5/5
  negatives: 4/4
  last-run: 2026-07-06T05:45:24Z
---

# Approved registries only

Flag dependency-source declarations that route around the organisation's
approved registry. Teams that run an internal registry mirror gate every
dependency through one policied chokepoint; a build file that
declares a direct public source - added in seconds by an agent resolving a
missing package - bypasses that chokepoint invisibly. This frame makes the
bypass visible at push time.

## What's detected

| File | Declaration |
|---|---|
| `pom.xml` | `<url>` inside `<repository>` / `<pluginRepository>` blocks |
| `build.gradle(.kts)`, `settings.gradle(.kts)` | `mavenCentral()`, `jcenter()`, `gradlePluginPortal()`, `google()`, and `url "https://..."` / `uri("https://...")` |
| `.npmrc` | `registry=` and scoped `@scope:registry=` lines |
| `.yarnrc.yml` | `npmRegistryServer:` |
| `requirements*.txt` | `--index-url`, `--extra-index-url`, `-i` |
| `pip.conf` / `pip.ini` | `index-url =`, `extra-index-url =` |

Hosts that are clearly private are never flagged: `localhost`, loopback
addresses, and bare intranet hostnames without a dot (`http://repo:8081`).

## The allowlist

Declare approved hosts in `.appframes/_canonical/approved-registries.toml`:

```toml
[registries]
mirror = "registry.corp.example.com"
proxy  = "artifacts.corp.example.com:8443"
# gradle shortcut names can be allowed by name:
# central = "mavenCentral"
```

- **Allowlist present:** only sources whose host (or gradle shortcut name) is
  NOT listed fire.
- **No allowlist:** every declared public registry fires as WARN inventory -
  the finding list IS the map of where your dependencies come from.

## Severity

`WARN`. Most open-source projects legitimately resolve from public
registries; the frame's job is visibility, not prohibition. Organisations
with a mirror policy escalate it to `BLOCK` via project severity tuning.

## Failure message

```
⚠ commands/approved-registries-only (commands)
   dependency sources outside the approved registries:
   - services/billing/pom.xml:41 - repo.maven.apache.org
   - agent-tools/.npmrc:2 - registry.npmjs.org
   fix: route dependencies through your internal registry mirror, or add
        the host to .appframes/_canonical/approved-registries.toml
```

## Override

Per-file disable:
```
# appframes:disable commands/approved-registries-only
```

Per-line disable (suppresses the line that follows the marker):
```
# appframes:disable-next-line commands/approved-registries-only
registry=https://registry.npmjs.org/
```

## What's NOT detected

- **Go module proxies** (`GOPROXY`) - configured via environment, not
  committed build files, in the common case.
- **`pyproject.toml` sources** (`[[tool.poetry.source]]`, `[tool.uv]`) -
  future expansion candidate.
- **Dockerfile `FROM` registries** - image provenance is a different concern
  from package registries; candidate for a sibling frame.
- **`<distributionManagement>` publish targets in pom.xml** - publishing is
  out of scope; the frame watches where code comes FROM.
