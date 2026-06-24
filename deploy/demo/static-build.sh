#!/usr/bin/env bash
# Snapshot a running read-only demo dashboard into a PURE STATIC site for
# Cloudflare Pages - no server, no container, no attack surface to watch.
# Crawls every internal link (nav + ?tab= / ?id= / ?repo= query-param links)
# into path-based static files and rewrites links so tabs and frame-detail
# pages click through. The actual crawl/rewrite is in static-build.py.
#
#   1. run the demo dashboard:  bash deploy/demo/run.sh   (→ :7902)
#   2. snapshot it:             bash deploy/demo/static-build.sh
#   3. deploy deploy/demo-static/ to CF Pages (drag-drop or `wrangler pages deploy`).
#
# Re-run to refresh (keeps the "minutes ago" timestamps from going stale).
set -euo pipefail

BASE="${BASE_URL:-http://127.0.0.1:7902}"
OUT="${OUT:-$(git rev-parse --show-toplevel)/deploy/demo-static}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

curl -fsS -o /dev/null "$BASE/" || { echo "no demo dashboard at $BASE - run deploy/demo/run.sh first" >&2; exit 1; }

python3 "$HERE/static-build.py" "$BASE" "$OUT"

# Tight CSP for the static site (everything is self-hosted; no external origins).
cat > "$OUT/_headers" <<'EOF'
/*
  Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; base-uri 'self'; form-action 'none'; frame-ancestors 'none'
  X-Content-Type-Options: nosniff
  X-Frame-Options: DENY
  Referrer-Policy: strict-origin-when-cross-origin
EOF
printf 'User-agent: *\nDisallow: /\n' > "$OUT/robots.txt"  # demo snapshot: don't index

# Social-share OG image (referenced by the meta injected in static-build.py).
cp "$HERE/og.png" "$OUT/og.png" 2>/dev/null || echo "  note: $HERE/og.png missing - skipping OG image" >&2

# Cloudflare Pages deploy config so the output dir is self-contained.
cat > "$OUT/wrangler.toml" <<'EOF'
# Cloudflare Pages config for demo.nimblegate.com - static snapshot, no build.
# Deploy from this directory:  wrangler pages deploy
# `name` must match your Cloudflare Pages project.
name = "nimblegate-demo"
pages_build_output_dir = "."
compatibility_date = "2025-01-01"
EOF
printf 'wrangler.toml\n' > "$OUT/.assetsignore"

echo "static demo built at $OUT - deploy to CF Pages."
