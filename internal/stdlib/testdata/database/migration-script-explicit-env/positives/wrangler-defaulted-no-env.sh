#!/bin/sh
# Positive case: SCOPE defaulted from ${1:-}, then wrangler invoked without
# an explicit --env / --remote flag. Hits all three conditions.
SCOPE="${1:-}"
wrangler d1 migrations apply mydb
