#!/bin/sh
# Negative: apply DDL THEN verify the new column exists by querying.
set -e
wrangler d1 execute "$DB" --remote --file migrations/0042_add_column.sql
wrangler d1 execute "$DB" --remote --command "SELECT name FROM pragma_table_info('users') WHERE name='email_verified_at';" | grep -q email_verified_at
echo "Migration applied AND verified."
