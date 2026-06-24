---
name: migration-script-explicit-env
category: database
subcategory: migrations
platform: [cloudflare, cf-pages]
framework: []
severity: BLOCK
tier: 1
tags: [migrations, cloudflare, cf-d1, cf-pages, env-vars, command-parse]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/*.sh"
    - "**/*.bash"
    - "**/*.zsh"
    - "**/scripts/**"
    - "**/apply-*-migration*"
dedup-key: "file:line"
pattern: multi-env-cli-silent-default
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 2/2
  negatives: 4/4
  last-run: 2026-05-24T00:00:00Z
---

# command-safety/migration-script-explicit-env

Reject bash scripts that invoke a multi-env CLI (wrangler, gcloud, kubectl, vercel, flyctl, supabase, firebase, heroku) without an explicit env-scope flag - when the script also has a defaulted `${1:-...}` env variable that resolves to empty.

Typical failure shape: a `wrangler d1 execute` call inside `apply-add-country-column-migration.sh` defaults `SCOPE=""` when the caller forgets to pass `prod`. `wrangler d1 execute` interprets no `--remote` as **local**, so the DDL only lands on local D1; production columns never exist. Every `/api/content` request 500s after deploy. Hours of debug + dirty prod data.

## What this catches

A multi-env CLI invocation in a shell script that has NO explicit env scope:

1. A multi-env CLI invocation: `wrangler`, `gcloud`, `kubectl`, `vercel`, `flyctl`, `fly`, `supabase`, `firebase`, `heroku`
2. The CLI line has NO explicit env flag (`--remote`, `--env`, `--project`, `--context`, `--account`, `--app`, `--namespace`, `--stage`)

The invocation then silently routes to the CLI's local / current-context environment - which is almost never what a migration / deploy script should do. A defaulted `SCOPE="${1:-}"` variable makes it worse (the label notes it), but the missing-flag invocation is the footgun.

## What this does NOT catch (precision)

- **`wrangler` deploy commands.** Only the data-plane subcommands (`d1`, `kv`, `r2`) have a local-vs-remote default. `wrangler pages deploy`, `wrangler deploy` (Workers), `versions`, `tail`, etc. always act on the remote - there is no local mode, so an env flag is meaningless. The frame skips wrangler lines whose subcommand isn't `d1`/`kv`/`r2`.
- **Validated scope variables.** If the script validates a variable against env-flag literals before use - e.g. `if [[ "$SCOPE" != "--local" && "$SCOPE" != "--remote" ]]; then exit 1; fi` - then passing `$SCOPE` to the CLI IS an explicit flag (indirect), and the frame treats it as handled. This is the exact safe pattern the Fix below recommends; the frame must not block its own recommended fix.

## Fix

Two changes, both required:

**1. Make the env arg required, not defaulted.**

```bash
# WRONG - bash defaults to empty when no arg passed
SCOPE="${1:-}"

# RIGHT - fail loudly if missing
SCOPE="${1:?usage: $0 <local|remote>}"
```

**2. Pass an explicit env flag to the CLI.**

```bash
# WRONG - wrangler defaults to local
wrangler d1 execute "$DB" --file=migration.sql

# RIGHT - explicit scope
wrangler d1 execute "$DB" --file=migration.sql --remote
```

For `gcloud`: `--project="$PROJECT"`. For `kubectl`: `--context="$CTX"`. For `vercel`: `--prod` or `--scope=team-name`. For `flyctl`: `--app="$APP"`.

## Suppressing intentional cases

When a script legitimately defaults to local (e.g. a dev convenience script that runs against the local emulator), suppress at the file level:

```bash
#!/usr/bin/env bash
# appframes:disable command-safety/migration-script-explicit-env
# (this script is dev-only; never run against production)
wrangler d1 execute "$DB" --file=fixture.sql
```

## Generalizes to

Any CLI with a local/remote or env-scoped default that's easy to get wrong:

- Cloudflare `wrangler` (D1, KV, R2, Pages, Workers) - defaults to local
- `gcloud` - picks active config project / region / account
- `kubectl` - current context determines target cluster
- `vercel` - `--prod` is opt-in; defaults to preview
- `flyctl` / `fly` - current app from `fly.toml` or env
- `supabase` - linked project
- `firebase` - `firebase use` default
- `heroku` - `--app` inferred from git remote

The frame fires on all of them when invoked from a shell script without an explicit env flag.
