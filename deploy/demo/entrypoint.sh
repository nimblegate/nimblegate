#!/usr/bin/env sh
# Demo container entrypoint: regenerate the fake seed (fresh "minutes ago"
# timestamps so the feed always looks live), then run the dashboard in
# READ-ONLY mode - --auth=off (no login wall) + NO --allow-edits (every
# mutation/POST route is unregistered, verified). Restart = clean re-seed.
set -e
bash /opt/demo/demo-seed.sh /srv/demo
exec nimblegate gateway dashboard --serve --auth=off \
  --policy-root /srv/demo --addr 0.0.0.0 --port 7900
