# Sign in

The gateway uses a single-admin model in v0.1.0: one username + password protects the dashboard. There's no self-registration; the admin account is created once during [Setup](/setup) via a setup token.

## Key actions

- **Sign in**: enter username + password.
- **Forgot password?**: there's no email reset. The recovery path is to print a new setup token from the container's logs (`docker logs nbg-eval | grep nbg-setup`) and re-claim admin at the setup URL.

## Common gotchas

- Sessions live for ~12 hours; you'll be re-prompted after that.
- The push side of the gate (SSH on port 2222) is independent of dashboard auth; agents push using SSH keys, not the admin password.

For depth: [README: Install](https://github.com/nimblegate/nimblegate/blob/main/README.md#install).
