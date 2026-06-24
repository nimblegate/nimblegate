# deploy/demo/ - read-only demo (container source + static public face)

A demo of the nimblegate dashboard over **fabricated seed data** - a visitor
clicks around the real dashboard (feed, repos, frames, stats, health) and
*feels* the product instead of reading about it, with no real repos,
credentials, or findings.

**Two outputs from one source:**

1. **The container** (`run.sh`) - runs the real dashboard read-only. Use it to
   run the demo locally yourself, and as the **render-source the static
   snapshot is built from**.
2. **The static snapshot** (`static-build.sh` → `deploy/demo-static/`) - the
   **public face**, deployed to Cloudflare Pages. THIS is what goes public,
   because a static site has **no server, no container, no attack surface to
   watch or patch** - which matters most for a *security* product's unattended
   public demo (a popped demo of a security tool is a brand disaster; a static
   CDN page has nothing to pop). It's a visual demo: clicking loads another
   pre-rendered page, query-param filters degrade to the default view, and a
   banner says so. Interested visitors install the real thing on their own
   machines anyway.

## Public deploy (recommended): static on CF Pages

The static snapshot lives in its **own repo** (`nimblegate/demo`), separate from
this product repo, so the public demo can ship/redeploy on its own cadence and
go public while the product source stays private. This generator stays here (it
renders the *real* dashboard, so it must track UI changes in lockstep); only the
built output goes to the demo repo, which Cloudflare Pages serves from its root.

First build (one time):

```bash
bash deploy/demo/run.sh                                 # 1. run the demo dashboard (→ :7902)
OUT=/path/to/nimblegate-demo bash deploy/demo/static-build.sh   # 2. snapshot into the demo repo
cd /path/to/nimblegate-demo && git add -A && git commit -m "demo snapshot" && git push
# 3. point a CF Pages project at the nimblegate/demo repo root (no build step)
```

### Refreshing - ⚠ do NOT point `OUT=` at the demo repo on a rebuild

`static-build.py` does `shutil.rmtree(OUT)` before writing, so re-running with
`OUT=<demo-repo>` would **delete that repo's `.git`** (history + remote lost).
Always build into a throwaway dir and **sync** into the repo, preserving `.git`:

```bash
bash deploy/demo/run.sh
OUT=/tmp/nbg-demo-build BASE_URL=http://127.0.0.1:7902 bash deploy/demo/static-build.sh
rsync -a --delete --exclude '.git' /tmp/nbg-demo-build/ /path/to/nimblegate-demo/
cd /path/to/nimblegate-demo && git add -A && git commit -m "refresh demo snapshot" && git push
```

The snapshot's "minutes ago" timestamps freeze at build time, so run the refresh
on a cron/CI (re-seed → rebuild → sync → push) to keep the feed looking live.

`deploy/demo-static/` (the default `OUT` when unset) is gitignored in this repo -
a scratch location for local previews, never committed here.

## Why this shape

- **It IS the product image**, not a mockup - so it can never drift stale from
  the real dashboard, and a visitor sees the genuine UI. (Same binary the
  cold-install eval container uses; one artifact, multiple jobs.)
- **Read-only by construction.** Run with `--auth=off` (no login wall) and
  WITHOUT `--allow-edits`. Every mutation/POST route (`/policy/repo/add`,
  credential rotation, ssh-keys, whitelist, …) is registered *only* under
  `--allow-edits`, so without it they return 404 - verified. A POST to a
  render-only route like `/repos` just re-renders; it mutates nothing.
- **Stateless + self-reseeding.** `demo-seed.sh` regenerates the policy-root on
  every container start with timestamps relative to *now*, so the feed always
  looks live ("7 minutes ago"). A restart is a clean reset - visitor clicks and
  the dashboard's own runtime files (`analytics.db`) never accumulate.
- **Honest fixtures.** The seed is fabricated-but-representative: a believable
  mix of blocked (live Stripe key, force-push to main, committed PEM key,
  non-idempotent migration, `rm -rf /`) and clean/observed pushes across three
  fake repos. Modest numbers, real shapes - NOT a hand-tuned highlight reel.

## Run locally

```bash
bash deploy/demo/run.sh          # build + run on 127.0.0.1:7902
# open http://127.0.0.1:7902
```

## Hourly reset (keeps timestamps live, discards drift)

```cron
0 * * * * docker restart nbg-demo
```

The entrypoint re-seeds on start, so a plain restart refreshes everything.

## Public deploy

The demo is a long-running HTTP server (not static), so it needs an always-on
host. Bind it to **localhost only** and front it with the existing TLS path -
`nimblegate gateway tls-setup` (Caddy) or a Cloudflare tunnel. **Never expose the
container port raw on `0.0.0.0`.** It's read-only with fake data, but the whole
nimblegate pitch is the security boundary, so the demo must model good posture.

## Files

- `demo-seed.sh` - generates the fake policy-root (3 repos, fresh timestamps).
- `entrypoint.sh` - re-seed, then exec the read-only dashboard.
- `Dockerfile` - multi-stage build → alpine + the binary + the two scripts.
- `run.sh` - build + (re)launch the `nbg-demo` container.
