---
name: doc-touches-with-code
category: documentation
subcategory: doc-drift
platform: []
framework: []
severity: WARN
tier: 6
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*"
canonical-refs:
  - code-doc-map.toml
pattern: drift-between-related-artifacts
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 2/2
  last-run: 2026-05-20T15:18:55Z
---

# Documentation must move with the code

A commit that changes source files declared in `code-doc-map.toml` must
also include a staged change to the mapped documentation file. The intent
is to keep documentation lockstep with the code it documents - when
implementation moves without docs, drift starts.

## When it fires

On `pre-commit` and on `nimblegate check`. The check loads
`.appframes/_canonical/code-doc-map.toml`, finds every entry whose glob
matches a staged source file, then verifies the entry's `docs` path is
also among the staged files. Missing docs entry → WARN, naming the
unmatched source path and its expected doc.

## Configuration

```toml
# .appframes/_canonical/code-doc-map.toml
[code-to-docs]
"internal/checks/*.go"     = "docs/frame-authoring.md"
"cmd/nimblegate/main.go"    = "README.md"
"internal/canonical/*.go"  = "docs/canonical-tables.md"
"docs/schemas/*.json"      = "docs/frame-authoring.md"
```

Globs follow `filepath.Match` semantics (no `**` - keep it simple in V0.5;
extend to `doublestar` patterns if needed).

## Failure message

```
⚠️  convention/doc-touches-with-code (convention)
   internal/checks/folderbranchlock.go was staged, but its mapped doc
   docs/frame-authoring.md was not. Either edit the doc to reflect the
   code change, or add [no-doc-update] to the commit message if the
   change is doc-irrelevant (refactor, rename, etc.).
```

## Override

Per-commit (most common): `[no-doc-update]` token anywhere in the commit
message. The override is recorded to the audit log with the staged source
paths so a future read of `nimblegate status` can show what slipped
through.

For larger refactors where docs land in a follow-up commit, prefer the
`[no-doc-update]` override over disabling the frame - keeps the audit
trail honest.

## Limitations (V0.5)

- Doesn't verify the doc change is *meaningful*; a one-character whitespace
  edit will satisfy this frame. The "is the doc current?" question is
  ultimately human review.
- Doesn't check that staged source + staged doc are *coherent* (i.e. you
  could update the doc to say something unrelated). Doc lint + spell check
  cover that, separately.
- Globs in the canonical table use `filepath.Match`, not full doublestar.

## Why this specific design

Two alternatives considered and rejected for V0.5:

1. **mtime-based heuristic.** "If `src/X.go` is newer than `docs/X.md`,
   warn." Fails because `git checkout` and `git clone` reset mtimes; the
   first invocation in a fresh checkout would warn for every doc.
2. **Doc-comment extraction.** "Parse Go AST, find every exported symbol
   without a comment, fail." Already covered by language-specific tools
   (`revive`, `golint`); not nimblegate's job. The frame `public-symbol-doc-comment`
   is a thin wrapper if a project wants one gate.

The canonical-map approach is the minimum mechanism that catches actual
drift with author-controlled false-positive rate.
