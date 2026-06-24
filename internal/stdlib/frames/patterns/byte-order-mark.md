---
id: byte-order-mark
description: UTF-8 byte-order mark (EF BB BF) at file start, unnecessary for UTF-8 and silently breaks shebangs, JSON parsers, and migration tools.
anticipated-siblings: []
---

# Pattern: byte-order-mark

UTF-8 has no byte order, so the BOM (EF BB BF, U+FEFF) at the start
of a file serves no purpose - but tooling that doesn't strip it
treats those three bytes as actual content. Common failure modes:

- Shell scripts: `#!/bin/bash` becomes `<BOM>#!/bin/bash`; the kernel
  doesn't recognize the shebang and runs the file under the parent
  shell, with subtly different behavior.
- JSON parsers: some reject the BOM with "unexpected token at
  position 0"; others silently include it in the first key name.
- Database migrations: the BOM appears as a leading character in the
  first statement, often producing cryptic "syntax error near EF".

Excel writes a BOM by default into CSV/TSV exports and these files
typically round-trip through it, so those extensions are exempt.
