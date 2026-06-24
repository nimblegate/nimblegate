#!/usr/bin/env bash
# Negative: SCOPE is defaulted from ${1:-} but VALIDATED against --local /
# --remote before use, then passed to wrangler. That is an explicit env flag
# (passed indirectly) - the exact safe pattern the frame's Fix recommends.
# Should NOT fire.
set -euo pipefail
DB_NAME="acme-studio"
SCOPE="${1:-}"
if [[ "$SCOPE" != "--local" && "$SCOPE" != "--remote" ]]; then
  echo "Usage: $0 --remote | --local" >&2
  exit 1
fi
wrangler d1 execute "$DB_NAME" $SCOPE --json --command "SELECT 1"
