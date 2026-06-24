# v2 Bucket Classification - 44 Concrete Stdlib Frames

> **Status:** DRAFT - Phase A Task A3 of the kit-architecture-three-axis implementation. Per plan, this file STOPS for user review before Phase B (filesystem moves) proceeds. Ambiguous classifications are marked with `⚠ AMBIGUOUS` and explained below.

> **Source:** `internal/stdlib/frames/` at branch `feat-kit-architecture-v2`. The 44 frames are those with `category` field in frontmatter. Pattern-taxonomy files in `patterns/` (15 files without category) are NOT executable gating frames per the v1 model and are out of scope for this classification.

> **Rule applied:** spec §3.1 primary-intent rule. Where a frame's existing frontmatter has `platform: [cloudflare, cf-pages]` etc., those signals informed but did not auto-determine the v2 bucket - the primary intent of the check still drives the assignment.

---

## Classification table

| v1 path | v2 bucket | Reasoning |
|---------|-----------|-----------|
| **core (8 frames - universal floor, opt-out only)** | | |
| git/no-force-push-main | core/no-force-push-main | Universal git invariant (LOCKED in spec §5.2) |
| git/no-bypass-pre-commit | core/no-bypass-pre-commit | Universal git invariant |
| git/no-amend-pushed-commits | core/no-amend-pushed-commits | Universal git invariant |
| git/folder-branch-lock | core/folder-branch-lock | Universal git invariant |
| filesystem/rm-rf-protected-paths | core/rm-rf-protected-paths | Universal filesystem safety (LOCKED in spec §5.2) |
| security/no-hardcoded-credentials | core/no-hardcoded-credentials | LOCKED to core per decision #3 (universality outweighs security-prefix) |
| security/no-private-keys-in-repo | core/no-private-keys-in-repo | LOCKED to core per decision #3 |
| commands/curl-pipe-shell | domains/security/curl-pipe-shell | Per spec §3.3 worked example: framework-agnostic command-execution security |
| **platform/cloudflare/cf-pages (4 frames - Pages-specific)** | | |
| app-correctness/top-of-page-import-safety | platform/cloudflare/cf-pages/top-of-page-import-safety | `platform: [cloudflare, cf-pages]` in frontmatter; Pages-specific bundling concern |
| app-correctness/prefer-static-public | platform/cloudflare/cf-pages/prefer-static-public | `platform: [cloudflare, cf-pages]` in frontmatter; Pages public dir convention |
| app-correctness/dynamic-env-declared | platform/cloudflare/cf-pages/dynamic-env-declared | `platform: [cloudflare, cf-pages]` in frontmatter; Pages env declaration |
| security/cf-pages-headers-baseline | platform/cloudflare/cf-pages/headers-baseline | `platform: [cloudflare, cf-pages]`; `_headers` file is Pages-specific (spec §3.2 example) |
| **platform/cloudflare/cf-d1 (2 frames - D1-specific)** | | |
| app-correctness/cf-graphql-schema-match | platform/cloudflare/cf-d1/graphql-schema-match | `platform: [cloudflare]` only; check is D1+GraphQL specific (per file's frontmatter `pattern: schema-vs-query-mismatch`) |
| app-correctness/cf-graphql-dataset-by-window | platform/cloudflare/cf-d1/graphql-dataset-by-window | `platform: [cloudflare]` only; D1+GraphQL window-query pattern |
| **domains/security (6 frames - vendor-agnostic security)** | | |
| security/no-innerHTML-user-input | domains/security/no-innerHTML-user-input | Framework-agnostic XSS pattern (spec §3.3 example) |
| security/no-mixed-content-urls | domains/security/no-mixed-content-urls | HTTPS mixed-content; framework-agnostic |
| security/no-bidi-override | domains/security/no-bidi-override | Bidi-attack pattern in source; framework-agnostic |
| security/no-invisible-tag-chars | domains/security/no-invisible-tag-chars | Invisible Unicode in source; framework-agnostic |
| security/no-zero-width-in-source | domains/security/no-zero-width-in-source | Zero-width chars in source code; framework-agnostic |
| security/no-homoglyph-identifiers | domains/security/no-homoglyph-identifiers | Look-alike chars in identifiers; framework-agnostic |
| **domains/html (4 frames - universal HTML well-formedness)** | | |
| convention/html-markup-valid | domains/html/markup-valid | Universal HTML well-formedness |
| convention/html-placeholder-content | domains/html/placeholder-content | Universal HTML hygiene |
| convention/html-img-alt | domains/html/img-alt | Universal HTML accessibility |
| convention/html-required-meta | domains/html/required-meta | Universal HTML structural (charset, viewport, title) - see ambiguity note below |
| **domains/seo (1 frame - search/social discoverability)** | | |
| convention/html-seo-meta | domains/seo/meta-tags-complete | Primary intent is search/social discoverability per spec §3.3 boundary rule (HTML vs SEO) |
| **domains/documentation (4 frames - docs hygiene)** | | |
| convention/dated-todo | domains/documentation/dated-todo | Documentation quality, framework-agnostic |
| convention/markdown-link-check-internal | domains/documentation/markdown-link-check-internal | Documentation link integrity |
| convention/doc-touches-with-code | domains/documentation/doc-touches-with-code | Docs-with-code policy, framework-agnostic |
| convention/cross-branch-id-consistency | domains/documentation/cross-branch-id-consistency | Doc-style consistency across branches |
| **domains/encoding (8 frames - universal encoding correctness)** | | |
| encoding/no-bom | domains/encoding/no-bom | Universal encoding hygiene |
| encoding/no-smart-quotes-in-config | domains/encoding/no-smart-quotes-in-config | Universal config-file encoding |
| encoding/yaml-no-tabs | domains/encoding/yaml-no-tabs | YAML spec compliance, universal |
| encoding/consistent-line-endings | domains/encoding/consistent-line-endings | Universal line-ending hygiene (also gets the binary-skip fix in Phase G) |
| encoding/no-mixed-indent | domains/encoding/no-mixed-indent | Universal indentation hygiene |
| encoding/no-en-dash-in-commands | domains/encoding/no-en-dash-in-commands | Universal command-line encoding |
| encoding/no-non-printable | domains/encoding/no-non-printable | Universal non-printable detection |
| encoding/no-zero-width-in-content | domains/encoding/no-zero-width-in-content | Universal zero-width detection in content |
| **domains/network (2 frames)** | | |
| network/cidr-host-bits-zero | domains/network/cidr-host-bits-zero | Network-config correctness, vendor-agnostic |
| network/no-localhost-in-proxy-config | domains/network/no-localhost-in-proxy-config | Network-config safety, vendor-agnostic |
| **domains/database (3 frames - vendor-agnostic DB safety)** | | |
| database/sqlite-migration-idempotent-wrapper | domains/database/sqlite-migration-idempotent-wrapper | Universal SQLite migration safety |
| database/schema-vs-code-drift | domains/database/schema-vs-code-drift | Universal schema-vs-code consistency |
| commands/apt-purge-preview | domains/filesystem/apt-purge-preview | Filesystem-destructive-command safety - see ambiguity below |

---

## ✓ Ambiguous classifications - ALL LOCKED 2026-06-06 via user review

All 6 ambiguities have been settled. Phase B (filesystem moves) proceeds with the recommendations marked LOCKED below. The migration translator's zero-delta gate will catch any classification that was wrong.

### Ambiguity #1: `database/migration-script-explicit-env` - **LOCKED: domains/database/migration-script-explicit-env**

Universal DB-migration safety pattern; the `platform: [cloudflare, cf-pages]` frontmatter tag reflects where it tends to fire, not where the check's intent lives. Operators on AWS RDS with similar patterns benefit equally.

### Ambiguity #2: `database/migration-verification-step` - **LOCKED: domains/database/migration-verification-step**

Same reasoning as #1 - universal DB-migration verification pattern, platform tag reflects test-fixture origin not intent.

### Ambiguity #3: `commands/apt-purge-preview` - **LOCKED: domains/filesystem/apt-purge-preview**

Creates the `domains/filesystem/` domain. Apt-purge is a filesystem-destructive command concern; not universal enough for `core/`, not security-shaped enough for `domains/security/`. Future filesystem-safety frames (delete-without-confirm, sudo-rm-anywhere, etc.) have a home ready under the new domain.

### Ambiguity #4: HTML structural vs SEO split - **LOCKED: split as-is (already pre-split in v1)**

The split the user requested already exists in the v1 catalog - `html-required-meta` checks ONLY charset/viewport/title (structural HTML); `html-seo-meta` checks description/canonical/og:*/twitter:* (SEO). My initial recommendation of "defer the split" was wrong - there's nothing to defer because v1 already split them. The classification routes them correctly:
- `convention/html-required-meta` → `domains/html/required-meta` (structural HTML only)
- `convention/html-seo-meta` → `domains/seo/meta-tags-complete` (SEO concerns)

No frame-authoring work needed. The split is at the classification routing level.

### Ambiguity #5: Create `domains/filesystem/` - **LOCKED: yes, create it**

Adds the new domain with `apt-purge-preview` as the first member. Symmetric with `domains/network/`, `domains/database/`, etc. - domain ready for future filesystem-safety frames.

### Ambiguity #6: `convention/` category dissolves - **LOCKED: confirmed**

The 9 v1 `convention/` frames split into 4 v2 buckets: 4 to `domains/html`, 1 to `domains/seo`, 4 to `domains/documentation`. The `convention/` top-level category disappears in v2 - an organizational artifact that doesn't survive the domain split.

---

## Frames by v2 bucket - summary count

| v2 bucket | Count | Frames |
|-----------|-------|--------|
| core/ | 7 | git-*, rm-rf-*, no-hardcoded-credentials, no-private-keys-in-repo |
| platform/cloudflare/cf-pages/ | 4 | top-of-page, prefer-static, dynamic-env, headers-baseline |
| platform/cloudflare/cf-d1/ | 2 | graphql-schema-match, graphql-dataset-by-window |
| domains/security/ | 7 | curl-pipe-shell, no-innerHTML, no-mixed-content, no-bidi-override, no-invisible-tag-chars, no-zero-width-in-source, no-homoglyph-identifiers |
| domains/html/ | 4 | markup-valid, placeholder-content, img-alt, required-meta |
| domains/seo/ | 1 | meta-tags-complete |
| domains/documentation/ | 4 | dated-todo, markdown-link-check-internal, doc-touches-with-code, cross-branch-id-consistency |
| domains/encoding/ | 8 | no-bom, no-smart-quotes, yaml-no-tabs, consistent-line-endings, no-mixed-indent, no-en-dash, no-non-printable, no-zero-width-in-content |
| domains/network/ | 2 | cidr-host-bits-zero, no-localhost-in-proxy-config |
| domains/database/ | 4 | migration-script-explicit-env, migration-verification-step, sqlite-migration-idempotent-wrapper, schema-vs-code-drift |
| domains/filesystem/ | 1 | apt-purge-preview |
| **TOTAL** | **44** | |

This matches the plan's working estimate (§A3 step 1): ~9 core, ~6 platform, ~3-5 framework (currently 0; framework axis grows later when Svelte/React-specific frames land), ~24-27 domains.

---

## Plan-flagged STOP

Per the implementation plan (Phase A Task A3 Step 1): **STOPS for user review BEFORE moving to Phase B (filesystem moves).**

The six ambiguities above need user decisions OR confirmation of my recommendations. After lock, Phase B proceeds with:
1. Move 44 frame markdown files into v2 layout per this table
2. Update frame frontmatter `id` fields to match new bucket paths
3. Add this `_classification.md` as the canonical history of which v1 path became which v2 bucket
4. Migration translator (Phase C) uses this same table

---

## Cross-refs

- Spec: `docs/superpowers/specs/2026-06-05-kit-architecture-three-axis-design.md` v2 (§3 primary-intent rule, §3.3 sibling domains, §5 core placement)
- Plan: `docs/superpowers/plans/2026-06-05-kit-architecture-three-axis.md` Phase A Task A3
- Parked HTML kit reshape: `.appframes/_future.md` - the §10.1 test case this classification supports
- Parked SEO kit reshape: `.appframes/_future.md` - the §10.2 test case (Ambiguity #4 informs Phase 2 work)
