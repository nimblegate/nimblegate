#!/bin/sh
# Positive: apply-*-migration.sh that uses --file= (the regex-detected
# DDL-apply form) but never queries the target afterward to verify the
# DDL took effect.
set -e
wrangler d1 execute "$DB" --remote --file=migrations/0042_add_column.sql
echo "Migration applied."
