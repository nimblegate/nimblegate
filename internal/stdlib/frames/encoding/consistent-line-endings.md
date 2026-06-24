---
name: consistent-line-endings
category: encoding
subcategory: line-endings
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
pattern: line-endings
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
---

# Consistent line endings

Block two specific line-ending hazards:

1. **Mixed CRLF + LF in the same file.** Almost always a Windows /
   Unix tool mixing on the same file. Diffs become noise; some parsers
   (notably make + bash + Python triple-quoted strings) handle it
   silently wrong.

2. **Unix shebang + CRLF anywhere in the file.** `#!/bin/bash\r` is
   the canonical "this script will not run" footgun - the kernel
   tries to invoke `/bin/bash\r` as the interpreter and prints
   `bad interpreter: no such file or directory`.

Windows-script extensions (`.bat`, `.cmd`, `.ps1`) and CRLF-required
formats are exempt.

## Fix

Normalize to LF:

```
dos2unix <file>
```

or with `sed`:

```
sed -i 's/\r$//' <file>
```

Lock the convention in `.gitattributes`:

```
* text=auto eol=lf
```
