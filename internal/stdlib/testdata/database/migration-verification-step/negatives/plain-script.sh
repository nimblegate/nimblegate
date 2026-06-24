#!/bin/sh
# Negative: not a migration script at all - naming doesn't match
# apply-*-migration*, so the frame skips it.
echo "deploying frontend"
npm run build
npm run deploy
