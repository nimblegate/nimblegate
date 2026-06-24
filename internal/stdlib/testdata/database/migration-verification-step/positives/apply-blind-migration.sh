#!/bin/sh
# Positive: blind migration apply with no readback step. Even worse: no
# error handling - a failed migration would still "succeed" here.
psql -f migrations/add_emails.sql
echo "Done."
