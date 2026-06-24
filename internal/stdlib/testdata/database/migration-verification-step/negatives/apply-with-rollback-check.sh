#!/bin/sh
# Negative: applies AND validates by selecting + checking row count.
set -e
psql -f migrations/add_emails.sql
COUNT=$(psql -tAc "SELECT count(*) FROM users WHERE email IS NOT NULL;")
echo "rows with email: $COUNT"
