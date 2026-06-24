#!/usr/bin/env bash
# Negative: `wrangler pages deploy` (and worker `deploy`) always act on the
# remote - there is no local mode and thus no local/remote footgun. An
# explicit env flag is meaningless here. Should NOT fire.
set -euo pipefail
wrangler pages deploy .svelte-kit/cloudflare \
  --project-name=studio-myapp \
  --branch=main
