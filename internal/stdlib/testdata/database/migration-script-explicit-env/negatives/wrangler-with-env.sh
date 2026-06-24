#!/bin/sh
# Negative: SCOPE defaulted but wrangler line carries --env, so the env
# is explicit. Should NOT fire.
SCOPE="${1:-}"
wrangler d1 migrations apply mydb --env staging
