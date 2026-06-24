# First-run setup

The very first time the gateway starts with no admin user, it generates a **setup token** and persists it. The token is printed to the container logs and stored at `/srv/gateway/cfg/_setup_token`. Only someone with shell access to the container or the on-disk policy volume can read it. That's the air-gap that prevents random web visitors from claiming admin.

## Key actions

1. **Find the token**: `docker logs <container> | grep "setup token"` or read `/srv/gateway/cfg/_setup_token` on the host.
2. **Open the setup URL**: <http://localhost:7900/setup>. On a **remote/headless** gateway the dashboard binds the host's loopback (it's the admin surface) - tunnel to it from your computer: `ssh -L 7900:127.0.0.1:7900 <user>@<gateway-host>` (use `127.0.0.1`, **not** `localhost` - it's published on IPv4), then open `http://localhost:7900/setup`. For a trusted LAN instead, set `NIMBLEGATE_DASHBOARD_HOST=0.0.0.0` and use `http://<box-ip>:7900`.
3. **Claim admin**: paste the token, pick any username + a password of 8+ characters.
4. **Sign in** at <http://localhost:7900/login>.

## What setup actually does

- Creates the admin user (bcrypt-hashed password).
- Consumes the setup token (one-time use; further setup-page visits redirect to login once a user exists).
- Records a "setup-claimed" event in [Events](/events).

## Common gotchas

- The token **persists across container restarts** until consumed; log lines marked `still-pending` mean the same unclaimed token from a prior boot. A fresh token only gets generated when there are zero users AND no token file yet.
- If you lose the password, the recovery path is to delete the auth DB so the gateway re-enters the no-users state, which generates a fresh token on next start. See [Login](/login)'s help.

For depth: [README: Install](https://github.com/nimblegate/nimblegate/blob/main/README.md#install).
