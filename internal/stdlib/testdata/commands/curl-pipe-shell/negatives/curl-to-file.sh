#!/bin/sh
# Negative: curl downloads to file, doesn't pipe to shell. Safe pattern -
# user can inspect before running.
curl -sSL -o install.sh https://example.com/install.sh
echo "Review install.sh, then run: sh install.sh"
