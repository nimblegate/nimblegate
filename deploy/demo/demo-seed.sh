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
#   bash demo-seed.sh <policy-root> [repos-root]
#
# With a repos-root, each repo also gets a real bare repo, and acme-storefront
# gets two agent branches that genuinely change the same files, so the Overlaps
# page shows a live pair and the recorded overlaps carry real commit SHAs.
set -euo pipefail

ROOT="${1:?usage: demo-seed.sh <policy-root> [repos-root]}"
REPOS="${2:-}"
mkdir -p "$ROOT"
find "$ROOT" -mindepth 1 -delete 2>/dev/null || true
if [ -n "$REPOS" ]; then
  mkdir -p "$REPOS"
  find "$REPOS" -mindepth 1 -delete 2>/dev/null || true
fi
export GIT_AUTHOR_NAME=agent GIT_AUTHOR_EMAIL=agent@example.com GIT_COMMITTER_NAME=agent GIT_COMMITTER_EMAIL=agent@example.com
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Create <repos-root>/<repo>.git with one commit of the given files on main.
# args: repo  file...
bare_repo() {
  local repo="$1"; shift
  local w="$WORK/$repo"
  git init -q -b main "$w"
  for f in "$@"; do mkdir -p "$w/$(dirname "$f")"; printf '// %s\n' "$f" > "$w/$f"; done
  git -C "$w" add -A && git -C "$w" commit -qm "Initial import"
  git init -q --bare "$REPOS/$repo.git"
  git -C "$REPOS/$repo.git" symbolic-ref HEAD refs/heads/main
  git -C "$w" push -q "$REPOS/$repo.git" main
}

# Commit an edit to the given files on branch (created from main if new), push
# it, and print the commit SHA.
# args: repo  branch  file...
agent_commit() {
  local repo="$1" branch="$2"; shift 2
  local w="$WORK/$repo"
  git -C "$w" checkout -q "$branch" 2>/dev/null || git -C "$w" checkout -q -b "$branch" main
  for f in "$@"; do printf '// %s\n' "$branch" >> "$w/$f"; done
  git -C "$w" commit -qam "Work on ${branch##*/}"
  git -C "$w" push -q "$REPOS/$repo.git" "$branch"
  git -C "$w" rev-parse HEAD
}

# RFC3339 UTC timestamp, N minutes ago (Go time.Time parses RFC3339).
ago() { date -u -d "-$1 minutes" +%Y-%m-%dT%H:%M:%SZ; }

# Append one JSON audit record (one line) to a repo's audit.log.
# args: repo  minutes_ago  ref  accept(true/false)  observed(true/false)  findings_json  suppressed_json  messages_json
rec() {
  local repo="$1" mins="$2" ref="$3" accept="$4" observed="$5" findings="$6" supp="$7" msgs="$8"
  local f="$ROOT/$repo/audit.log"
  printf '{"time":"%s","repo":"%s","refs":["%s"],"ref_updates":[{"Name":"%s","OldRev":"a1b2c3d","NewRev":"%s"}],"accept":%s,"observed":%s,"findings":%s,"suppressed":%s,"messages":%s}\n' \
    "$(ago "$mins")" "$repo" "$ref" "$ref" "${NEWREV:-e4f5a6b}" "$accept" "$observed" "$findings" "$supp" "$msgs" >> "$f"
}

# An accepted push that changes files another open agent branch also changes.
# args: repo  minutes_ago  ref  sha  other_ref  other_sha  files_json
rec_overlap() {
  local repo="$1" mins="$2" ref="$3" sha="$4" other="$5" other_sha="$6" files="$7"
  printf '{"time":"%s","repo":"%s","refs":["%s"],"ref_updates":[{"Name":"%s","OldRev":"0000000000000000000000000000000000000000","NewRev":"%s"}],"accept":true,"overlaps":[{"ref":"%s","other_ref":"%s","files":%s,"sha":"%s","other_sha":"%s"}]}\n' \
    "$(ago "$mins")" "$repo" "$ref" "$ref" "$sha" "$ref" "$other" "$files" "$sha" "$other_sha" >> "$ROOT/$repo/audit.log"
}

seed_repo() {
  local repo="$1" upstream="$2" frames="$3"
  mkdir -p "$ROOT/$repo"
  printf 'upstream-url = "%s"\nenabled = true\n' "$upstream" > "$ROOT/$repo/gateway.toml"
  printf '[frames]\nenabled = [%s]\n' "$frames" > "$ROOT/$repo/appframes.toml"
  : > "$ROOT/$repo/audit.log"
}

NONE='[]'

# ---- acme-storefront: e-commerce, the credential + force-push story ----
seed_repo "acme-storefront" "git@git.example.com:acme/storefront.git" '"@tier-1", "@web", "@security-strict"'
rec acme-storefront 7   refs/heads/feat-checkout true false "$NONE" "$NONE" '[]'
rec acme-storefront 34  refs/heads/main false false \
  '[{"id":"security/no-hardcoded-credentials","severity":"BLOCK","message":"config/payments.js:14 - Stripe secret key (live)"}]' \
  "$NONE" '["push rejected for acme-storefront"]'
rec acme-storefront 96  refs/heads/main false false \
  '[{"id":"git/no-force-push-main","severity":"BLOCK","message":"refs/heads/main: non-fast-forward (force-push) to a protected branch"}]' \
  "$NONE" '["push rejected for acme-storefront"]'
rec acme-storefront 210 refs/heads/feat-cart true false "$NONE" \
  '[{"frame":"documentation/dated-todo","file":"src/cart.js","label":"known backlog item","severity":"WARN"}]' '[]'
rec acme-storefront 1490 refs/heads/feat-search true false "$NONE" "$NONE" '[]'
# Two agents working in parallel end up in the same files. With a repos-root
# these are real commits on real branches; without one, fixed stand-in SHAs.
TAX1=3f8b2c7d1e9a4f6b0c5d8e2a7b1f4c9d6e3a0b52 COUPONS=7c1e9a4b2d8f3e6a0b5c9d1e4f7a2b8c3d6e9f01 TAX2=9d4a1f7c3e8b2d6a5f0c9e1b4d7a3f8c2e6b0d19
if [ -n "$REPOS" ]; then
  bare_repo acme-storefront src/checkout/total.js src/checkout/total.test.js src/cart.js README.md
  TAX1=$(agent_commit acme-storefront agent/claude/checkout-tax src/checkout/total.js)
  COUPONS=$(agent_commit acme-storefront agent/cursor/checkout-coupons src/checkout/total.js src/checkout/total.test.js)
  TAX2=$(agent_commit acme-storefront agent/claude/checkout-tax src/checkout/total.test.js)
fi
NEWREV=$TAX1 rec acme-storefront 26 refs/heads/agent/claude/checkout-tax true false "$NONE" "$NONE" '[]'
rec_overlap acme-storefront 15 refs/heads/agent/cursor/checkout-coupons "$COUPONS" \
  refs/heads/agent/claude/checkout-tax "$TAX1" '["src/checkout/total.js"]'
rec_overlap acme-storefront 4 refs/heads/agent/claude/checkout-tax "$TAX2" \
  refs/heads/agent/cursor/checkout-coupons "$COUPONS" '["src/checkout/total.js","src/checkout/total.test.js"]'

# ---- payments-api: backend, the private-key + migration story ----
[ -n "$REPOS" ] && bare_repo payments-api cmd/api/main.go migrations/008_payouts.sql README.md
seed_repo "payments-api" "git@git.example.com:acme/payments-api.git" '"@tier-1", "@migrations", "@security-strict"'
cat >> "$ROOT/payments-api/appframes.toml" <<'LINTERS'

[linters]
  [linters.no-em-dash]
    kind = "regex"
    enabled = true
    severity = "WARN"
    patterns = ["*"]
    regex = "\\x{2014}"
LINTERS
rec payments-api 19  refs/heads/main false false \
  '[{"id":"security/no-private-keys-in-repo","severity":"BLOCK","message":"deploy/release.pem - PEM RSA private key"}]' \
  "$NONE" '["push rejected for payments-api"]'
rec payments-api 142 refs/heads/feat-payouts false false \
  '[{"id":"database/sqlite-migration-idempotent-wrapper","severity":"BLOCK","message":"migrations/008_payouts.sql:5 - DROP TABLE without IF EXISTS (non-idempotent)"}]' \
  "$NONE" '["push rejected for payments-api"]'
rec payments-api 320 refs/heads/feat-payouts true false "$NONE" \
  '[{"frame":"security/no-hardcoded-credentials","file":"test/fixtures/stripe_test.go","label":"known-fake test fixtures","severity":"BLOCK"},{"frame":"security/no-private-keys-in-repo","file":"test/fixtures/dummy.pem","label":"known-fake test fixtures","severity":"BLOCK"},{"frame":"app-correctness/no-em-dash","file":"docs/api/webhooks.md","severity":"WARN","origin":"linter"},{"frame":"app-correctness/no-em-dash","file":"docs/api/payouts.md","severity":"WARN","origin":"linter"},{"frame":"app-correctness/no-em-dash","file":"README.md","severity":"WARN","origin":"linter"}]' '[]'
rec payments-api 880 refs/heads/main true false \
  '[{"id":"app-correctness/no-em-dash","severity":"WARN","origin":"linter","message":"docs/api/refunds.md:22 - em dash in prose"}]' \
  "$NONE" '[]'

# ---- marketing-site: static/web, observe-mode + rm-rf story ----
[ -n "$REPOS" ] && bare_repo marketing-site index.html deploy.sh
seed_repo "marketing-site" "git@git.example.com:acme/marketing-site.git" '"@tier-1", "@web", "@cf-pages"'
echo 'observe = true' >> "$ROOT/marketing-site/gateway.toml"
rec marketing-site 12  refs/heads/main true false "$NONE" "$NONE" '[]'
rec marketing-site 58  refs/heads/redesign true true \
  '[{"id":"web/html-required-meta","severity":"WARN","message":"index.html - missing meta description"}]' \
  "$NONE" '[]'
rec marketing-site 240 refs/heads/main false false \
  '[{"id":"filesystem/rm-rf-protected-paths","severity":"BLOCK","message":"deploy.sh:11 - rm -rf on a protected path (/)"}]' \
  "$NONE" '["push rejected for marketing-site"]'
rec marketing-site 2010 refs/heads/redesign true false "$NONE" "$NONE" '[]'

echo "seeded demo policy-root at $ROOT ($(find "$ROOT" -name audit.log | wc -l) repos)"
