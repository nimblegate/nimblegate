---
name: no-bom
category: encoding
subcategory: byte-order-mark
platform: []
framework: []
severity: BLOCK
severity-source: frame
tier: 2
dedup-key: file
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*"
pattern: byte-order-mark
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
---

# No UTF-8 byte-order mark

Reject text files that begin with the UTF-8 byte-order mark (EF BB BF,
U+FEFF). The BOM is unnecessary for UTF-8 and silently breaks shell
scripts (interpreter line not recognised), shebang handling, JSON
parsers, and many migration tools.

CSV and TSV files are exempt - Excel writes a BOM by default and the
files often round-trip through it.

## Fix

Strip the BOM:

```
sed -i '1s/^\xEF\xBB\xBF//' <file>
```

or in editors with "save without BOM" / "encoding: UTF-8 (no BOM)".

If a specific file legitimately needs a BOM (rare Windows tooling),
suppress at the file level with `# appframes:disable encoding/no-bom`.
