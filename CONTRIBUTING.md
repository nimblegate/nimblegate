# Contributing to nimblegate

Thanks for considering a contribution.

## What's welcome

- **New stdlib frames:** markdown + Go check function + positives/negatives testdata. See `docs/frame-catalog.md` and existing frames under `internal/stdlib/frames/` for the pattern.
- **Doc improvements:** especially the quickstart, frame catalog, and onboarding.
- **Bug fixes** with a reproducing test.
- **Performance improvements** with a before/after benchmark.

## What to discuss first

Ping me in an issue before:

- Major refactors of `internal/gateway/` or `internal/commands/`
- New external dependencies (the project deliberately stays stdlib-heavy)
- Anything that touches the agent-proof premise (audit log mutations, gate bypass paths, hook installation)

## Process

1. Fork + branch off `main`
2. `go test ./...` must pass before PR
3. Pre-commit gate runs the agent-proof checks; **don't** `--no-verify`
4. Co-author commits if you're agent-assisted (the project uses `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>` as the convention; substitute your agent / model if different)

## License + CLA

By contributing, you agree your contribution is licensed under the PolyForm Noncommercial License 1.0.0 (the project's source license).

The project is dual-licensed (PolyForm Noncommercial 1.0.0 for source-available distribution + a commercial license for commercial customers). To keep the commercial license offering viable, **external contributions will require signing a Contributor License Agreement (CLA)** that grants the project the right to relicense the contribution as part of the dual-license model. The CLA Assistant bot will be wired up before the first external PR is merged; until then, the project welcomes discussion-stage contributions but defers merging until CLA is in place. If your contribution is small (typo, doc edit) the CLA process will be a one-click sign-off.
