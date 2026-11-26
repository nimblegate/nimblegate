# Sign in

The gateway uses a single-admin model: one username + password protects the dashboard. There's no self-registration; the admin account is created once during [Setup](/setup) via a setup token.

## Key actions

- **Sign in**: enter username + password.
- **Forgot password?**: there's no email reset. Delete the auth DB and restart the dashboard, which then prints a new setup token; re-claim admin at `/setup`. Repos, keys and audit logs are kept. Docker: `docker exec <container> sh -c 'rm -f /srv/gateway/cfg/_auth.db /srv/gateway/cfg/_auth.db-shm /srv/gateway/cfg/_auth.db-wal'`, `docker restart <container>`, then `docker logs <container> | grep nbg-setup`. Bare metal: stop `nimblegate-dashboard.service`, remove the same three files, start it, then `nimblegate gateway setup-token --policy-root /srv/gateway/cfg`. Deleting the DB also removes agent API tokens; mint new ones afterwards.

## Common gotchas

- Sessions live for ~12 hours; you'll be re-prompted after that.
- The push side of the gate (SSH on port 2222) is independent of dashboard auth; agents push using SSH keys, not the admin password.

For depth: [Quick start](https://nimblegate.com/docs/quick-start).
