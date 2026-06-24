#!/bin/sh
# Negative: curl + checksum verification before execution.
curl -sSL -o /tmp/install.sh https://example.com/install.sh
echo "abc123  /tmp/install.sh" | sha256sum -c
sh /tmp/install.sh
