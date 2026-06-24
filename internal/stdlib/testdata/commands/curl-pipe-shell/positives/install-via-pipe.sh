#!/bin/sh
# Positive: classic curl-pipe-shell pattern downloading and executing
# arbitrary code without integrity verification.
curl -sSL https://example.com/install.sh | sh
