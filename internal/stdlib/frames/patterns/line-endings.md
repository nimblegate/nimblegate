---
id: line-endings
description: Inconsistent or platform-wrong line endings (mixed CRLF + LF, or Unix shebang + CRLF) that break parsers and interpreter loading.
anticipated-siblings: []
---

# Pattern: line-endings

Two failure modes in one shape:

1. **Mixed CRLF + LF in the same file.** Usually a Windows / Unix
   tool mixing on the same file. Diffs become noise; some parsers
   (notably `make`, `bash` heredocs, Python triple-quoted strings)
   handle the difference silently wrong rather than erroring.
2. **Unix shebang + CRLF.** `#!/bin/bash\r` is the canonical
   "this script will not run" footgun - the kernel parses the
   interpreter path as `/bin/bash\r` (with CR), tries to invoke
   that, and prints `bad interpreter: no such file or directory`.

The defense is to commit to one line-ending style per project
(usually LF on non-Windows codebases) and lock it via `.gitattributes`.
Windows-script extensions (`.bat`, `.cmd`, `.ps1`) are exempt because
they require CRLF.
