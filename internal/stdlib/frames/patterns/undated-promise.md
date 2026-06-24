---
id: undated-promise
description: TODO, FIXME, HACK, or XXX marker without ownership or expiry date - promises with no enforcement.
anticipated-siblings: []
---

# Pattern: undated-promise

`// TODO: clean this up later` - every codebase has hundreds. None get cleaned up. The marker was a sincere intent at write-time and then became invisible. Years later the original author is gone; the "TODO" sits unchanged, signaling "this code is broken" to every new reader without enabling action.

The structural defense: require every TODO / FIXME / HACK / XXX to carry an owner and an expiry date (`TODO(@alice 2026-08): rework error handling`). Untagged markers either get enriched at commit time or stripped. The cost is a few seconds per marker; the savings is a codebase where unfinished items have provenance and a clock.
