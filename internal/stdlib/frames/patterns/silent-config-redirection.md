---
id: silent-config-redirection
description: Committed config file controls where data flows; a small change silently redirects future operations to an attacker-controlled endpoint without surface warning.
anticipated-siblings: []
---

# Pattern: silent-config-redirection

A repository commits a config file that declares an endpoint - where uploads go, which server resolves a service, what registry packages come from. The endpoint isn't inline in the code path that uses it; it's resolved at runtime by a tool that reads the committed config. A single-line change to the URL silently redirects every future operation that depends on the config, with no surface warning to the user invoking the operation.

The structural defense is to treat any change to the redirection-controlling file as itself a gated action - block the commit, require an out-of-band acknowledgment, and surface the change in audit so the redirection happens with informed consent rather than as a quiet effect of a normal-looking config edit.

Examples in the wild:
- `.lfsconfig` declaring the LFS server URL - change it, future binary uploads go elsewhere
- `.npmrc` declaring `registry=` - change it, future installs pull from elsewhere
- `pip.conf` declaring `index-url` - change it, future installs pull from elsewhere
- `~/.docker/config.json` `auths.<registry>` - change it, future pulls authenticate elsewhere
- `.gitconfig`'s `[url "X"] insteadOf = Y` - change it, future clone of Y goes to X
- Maven `settings.xml` `<mirrors>` - change it, future builds resolve elsewhere

Each is a single-line edit in a file that looks like ordinary configuration but controls where the trusted operation actually goes. The catastrophic case is the same shape across all of them: silent + persistent + invisible at use time, only detected during incident response after an unrelated signal.
