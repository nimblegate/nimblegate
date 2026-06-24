#!/usr/bin/env bash
# Build + (re)launch the public demo container, read-only, on $PORT (default 7902).
# Stateless: every start re-seeds fresh. Reset hourly with cron:
#   0 * * * * docker restart nbg-demo
set -euo pipefail
PORT="${PORT:-7902}"
cd "$(git rev-parse --show-toplevel)"
docker build -f deploy/demo/Dockerfile -t nimblegate-demo --build-arg VERSION="$(git rev-parse --short HEAD)-demo" .
docker rm -f nbg-demo >/dev/null 2>&1 || true
docker run -d --name nbg-demo --restart unless-stopped -p "127.0.0.1:${PORT}:7900" nimblegate-demo >/dev/null
echo "demo on http://127.0.0.1:${PORT}  (public deploy: front with Caddy/CF tunnel, never bind 0.0.0.0 raw)"
