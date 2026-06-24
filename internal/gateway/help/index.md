# Dashboard

nimblegate sits between your AI agent's `git push` and your real upstream repo. This dashboard is the control plane: you register repos, pick which rules run against pushes, watch what's happening live, and see what was prevented.

## Where to start

- **First time setting up?** Add an SSH key at [SSH keys](/ssh-keys), register a repo at [Repos](/repos), then refine its rules at [Policy](/policy).
- **Watching pushes live?** [Feed](/feed) shows every decision as it happens; [Events](/events) is the raw audit log.
- **Picking rules?** [Policy](/policy) is where you tick frames on/off per repo. [Frames](/frames) is the read-only browse view of every rule that exists.
- **Measuring impact?** [Stats](/stats) shows time-prevented per week + which rules pull their weight.

## Common gotchas

- The left rail collapses on narrow screens; tap the hamburger to expand.
- Repo-scoped pages (Policy, Feed, Stats) honor the repo dropdown at the top right.

For depth: [README](https://github.com/nimblegate/nimblegate/blob/main/README.md).
