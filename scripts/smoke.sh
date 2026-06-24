#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$PWD"

echo "==> Build"
go build -ldflags "-X nimblegate/internal/version.Version=0.0.0-dev" -o nimblegate ./cmd/nimblegate

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "==> Init project at $TMP"
cd "$TMP"
git init -b main -q
git config user.email "smoke@example.com"
git config user.name "smoke"
"$ROOT/nimblegate" init

echo "==> Apply attack-class kit (security-strict, 9 frames)"
"$ROOT/nimblegate" kits apply security-strict

echo "==> Apply paste-corruption kit (encoding-strict, 8 frames)"
"$ROOT/nimblegate" kits apply encoding-strict

echo "==> Clean check (no violations)"
"$ROOT/nimblegate" check

echo "==> Establish a baseline commit (so the cleanup commit later has a parent)"
export PATH="$ROOT:$PATH"
echo "# smoke" > README.md
git add README.md
git commit -q -m "smoke: baseline"

# -------------------------------------------------------------------
# Test push - Trojan Source attack (CVE-2021-42574).
# Demonstrates the headline new frame: security/no-bidi-override.
#
# A U+202E (RLO) byte makes the source RENDER one control-flow path
# to a human reviewer but EXECUTE a different one. AI-pasted code
# is a primary delivery vector - the override character survives
# clipboard copy without showing in most editors.
#
# We generate the U+202E byte at runtime via printf so this script
# itself stays free of bidi-control bytes (otherwise nimblegate
# would flag the smoke script when run against the nimblegate repo).
# -------------------------------------------------------------------
echo "==> Introduce a Trojan Source attack (U+202E RLO in auth.py)"
RLO=$(printf '\xe2\x80\xae')
cat > auth.py <<EOF
def grant_admin(user):
    # appears as: if user.is_admin and not banned: grant()
    # executes as: if user.is_admin${RLO} and not banned: grant()
    if user.is_admin${RLO} and not banned:
        grant()
EOF
git add auth.py

echo "==> Run check (expecting exit 1 - security/no-bidi-override blocks)"
set +e
OUT=$("$ROOT/nimblegate" check 2>&1)
EXIT=$?
set -e
echo "$OUT" | tail -10
if [ "$EXIT" -ne 1 ]; then
    echo "expected exit 1, got $EXIT" >&2
    exit 1
fi
if ! echo "$OUT" | grep -q "security/no-bidi-override"; then
    echo "expected security/no-bidi-override in output" >&2
    exit 1
fi
if ! echo "$OUT" | grep -q "U+202E"; then
    echo "expected codepoint U+202E in output" >&2
    exit 1
fi

echo "==> Verify pre-commit hook blocks the Trojan Source commit"
set +e
git commit -m "should be blocked - Trojan Source" 2>&1 | tail -5
COMMIT_EXIT=${PIPESTATUS[0]}
set -e
if [ "$COMMIT_EXIT" -eq 0 ]; then
    echo "expected commit to be blocked, but it succeeded" >&2
    exit 1
fi

echo "==> Remove the Trojan Source file (real fix - never suppress this one)"
rm auth.py
git rm -qf auth.py 2>/dev/null || git restore --staged auth.py 2>/dev/null || true

echo "==> Verify check is clean after the fix"
"$ROOT/nimblegate" check

echo "==> Verify a clean commit succeeds (gate didn't get stuck)"
echo "ok" > NOTES.md
git add NOTES.md
git commit -q -m "smoke: ok (post-cleanup)"

echo "==> Smoke OK"
