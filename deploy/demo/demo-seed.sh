#!/usr/bin/env bash
# Seed a throwaway demo policy-root with realistic, FAKE gateway data so the
# read-only dashboard shows the real "what was prevented" story instead of an
# empty setup wizard. No real repos, no real credentials, no real findings -
# every record below is fabricated-but-representative (honest shape, modest
# numbers; NOT a hand-tuned highlight reel).
#
# Timestamps are computed relative to NOW at seed time, so the feed always
# looks live (most recent push "minutes ago"). Re-run / restart re-seeds fresh.
#
#   bash demo-seed.sh <policy-root>      # writes <root>/<repo>/{gateway.toml,appframes.toml,audit.log}
set -euo pipefail

ROOT="${1:?usage: demo-seed.sh <policy-root>}"
mkdir -p "$ROOT"
find "$ROOT" -mindepth 1 -delete 2>/dev/null || true

# RFC3339 UTC timestamp, N minutes ago (Go time.Time parses RFC3339).
ago() { date -u -d "-$1 minutes" +%Y-%m-%dT%H:%M:%SZ; }

# Append one JSON audit record (one line) to a repo's audit.log.
# args: repo  minutes_ago  ref  accept(true/false)  observed(true/false)  findings_json  suppressed_json  messages_json
rec() {
  local repo="$1" mins="$2" ref="$3" accept="$4" observed="$5" findings="$6" supp="$7" msgs="$8"
  local f="$ROOT/$repo/audit.log"
  printf '{"time":"%s","repo":"%s","refs":["%s"],"ref_updates":[{"Name":"%s","OldRev":"a1b2c3d","NewRev":"e4f5a6b"}],"accept":%s,"observed":%s,"findings":%s,"suppressed":%s,"messages":%s}\n' \
    "$(ago "$mins")" "$repo" "$ref" "$ref" "$accept" "$observed" "$findings" "$supp" "$msgs" >> "$f"
}

seed_repo() {
  local repo="$1" upstream="$2" frames="$3"
  mkdir -p "$ROOT/$repo"
  printf 'upstream-url = "%s"\n' "$upstream" > "$ROOT/$repo/gateway.toml"
  printf '[frames]\nenabled = [%s]\n' "$frames" > "$ROOT/$repo/appframes.toml"
  : > "$ROOT/$repo/audit.log"
}

NONE='[]'

# ---- acme-storefront: e-commerce, the credential + force-push story ----
seed_repo "acme-storefront" "https://github.com/acme/storefront.git" '"@tier-1", "@web", "@security-strict"'
rec acme-storefront 7   refs/heads/feat-checkout true false "$NONE" "$NONE" '[]'
rec acme-storefront 34  refs/heads/main false false \
  '[{"id":"security/no-hardcoded-credentials","severity":"BLOCK","message":"config/payments.js:14 - Stripe secret key (live)"}]' \
  "$NONE" '["push rejected for acme-storefront"]'
rec acme-storefront 96  refs/heads/main false false \
  '[{"id":"git/no-force-push-main","severity":"BLOCK","message":"refs/heads/main: non-fast-forward (force-push) to a protected branch"}]' \
  "$NONE" '["push rejected for acme-storefront"]'
rec acme-storefront 210 refs/heads/feat-cart true false "$NONE" \
  '[{"frame":"documentation/dated-todo","file":"src/cart.js","label":"known backlog item"}]' '[]'
rec acme-storefront 1490 refs/heads/feat-search true false "$NONE" "$NONE" '[]'

# ---- payments-api: backend, the private-key + migration story ----
seed_repo "payments-api" "https://github.com/acme/payments-api.git" '"@tier-1", "@migrations", "@security-strict"'
rec payments-api 19  refs/heads/main false false \
  '[{"id":"security/no-private-keys-in-repo","severity":"BLOCK","message":"deploy/release.pem - PEM RSA private key"}]' \
  "$NONE" '["push rejected for payments-api"]'
rec payments-api 142 refs/heads/feat-payouts false false \
  '[{"id":"database/sqlite-migration-idempotent-wrapper","severity":"BLOCK","message":"migrations/008_payouts.sql:5 - DROP TABLE without IF EXISTS (non-idempotent)"}]' \
  "$NONE" '["push rejected for payments-api"]'
rec payments-api 320 refs/heads/feat-payouts true false "$NONE" "$NONE" '[]'
rec payments-api 880 refs/heads/main true false "$NONE" "$NONE" '[]'

# ---- marketing-site: static/web, observe-mode + rm-rf story ----
seed_repo "marketing-site" "https://github.com/acme/marketing-site.git" '"@tier-1", "@web", "@cf-pages"'
rec marketing-site 12  refs/heads/main true false "$NONE" "$NONE" '[]'
rec marketing-site 58  refs/heads/redesign true true \
  '[{"id":"web/html-required-meta","severity":"WARN","message":"index.html - missing meta description"}]' \
  "$NONE" '[]'
rec marketing-site 240 refs/heads/main false false \
  '[{"id":"filesystem/rm-rf-protected-paths","severity":"BLOCK","message":"deploy.sh:11 - rm -rf on a protected path (/)"}]' \
  "$NONE" '["push rejected for marketing-site"]'
rec marketing-site 2010 refs/heads/redesign true false "$NONE" "$NONE" '[]'

echo "seeded demo policy-root at $ROOT ($(find "$ROOT" -name audit.log | wc -l) repos)"
