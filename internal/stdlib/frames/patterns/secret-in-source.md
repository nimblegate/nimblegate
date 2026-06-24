---
id: secret-in-source
description: Credentials, keys, or sensitive tokens committed to source control.
anticipated-siblings: []
---

# Pattern: secret-in-source

API tokens, private keys, OAuth client secrets, database passwords - once committed to git, they live forever in the history. Even after rotation, the old value is fetchable from any clone. Public repos make this an immediate compromise; private repos make it a slow leak as access scopes change.

The structural defense is two-layered: detect known-prefix tokens (AWS, GitHub, Stripe, Slack, etc.) and known key formats (PEM headers, common filename patterns); refuse to commit them and refuse to load them from already-committed files for runtime use. The first layer prevents new leaks; the second flags historical ones that need rotation.
