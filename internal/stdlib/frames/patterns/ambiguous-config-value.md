---
id: ambiguous-config-value
description: Config value that allows multiple valid interpretations, leading to non-deterministic or surprising behavior.
anticipated-siblings: []
---

# Pattern: ambiguous-config-value

A CIDR like `10.0.0.5/24` is valid syntax - but does the user mean the network 10.0.0.0/24, or the host 10.0.0.5 in that network? Both are defensible reads; tooling picks one and the user assumed the other. Same shape: `localhost` in a proxy config resolves to either 127.0.0.1 or [::1] depending on the resolver. `0.0.0.0` as a bind address means "all interfaces" but as a destination means "this host."

The structural defense: detect the ambiguous shapes at config-write time and require disambiguation. Force `10.0.0.0/24` (network) OR `10.0.0.5/32` (host); force `127.0.0.1` OR `[::1]`. The cost is a small annotation; the savings is a class of "works on my machine" bugs.
