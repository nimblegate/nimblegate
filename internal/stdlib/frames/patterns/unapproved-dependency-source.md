---
id: unapproved-dependency-source
description: Dependency or artifact source declared outside the organisation's approved registry.
anticipated-siblings: []
---

# Pattern: unapproved-dependency-source

A build file quietly gains a `<repository>` URL, a `mavenCentral()` call, a
`registry=` line, an `--extra-index-url` flag - and from that moment the
project pulls code from a source nobody vetted. Organisations that run an
internal registry mirror (Nexus, Artifactory) do it precisely so every
dependency passes one policied chokepoint; a direct-to-public source in a
build file routes around that chokepoint entirely.

The risk is sharpest with AI agents: an agent adding a dependency will happily
also add the registry declaration that makes it resolve, and a human reviewer
skims build-file diffs. Dependency-confusion attacks exploit exactly this
gap - a public package shadowing an internal name wins the race when a build
consults public and private sources side by side.

The structural defense: declare the approved hosts once, flag every dependency
source that isn't one of them. The alternative isn't avoiding public
registries - it's making the mirror the only road.
