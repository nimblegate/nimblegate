# SSH keys

Authorized public keys for the gateway. Every `git push` against the gateway runs over SSH; the agent must hold the private key matching one of these public keys.

## Key actions

- **Add a key**: paste the public key (one line, `ssh-ed25519 AAAA... user@host`). It's stored mode 0600 in the container's ssh volume.
- **Delete a key**: removes it from `authorized_keys`; immediate, no grace period. Active sessions stay connected; new pushes need a different key.
- **Name a key**: the comment at the end of the key line (`user@host`) shows in the list here, so you can tell keys apart. Pushes are not attributed to a key: the audit log and [Events](/events) do not record which key pushed (sshd's own log does). One key per agent still lets you revoke a single agent's access without touching the others.

## Don't have an SSH key yet?

One command, press Enter at each prompt to accept defaults; pick a passphrase or leave blank:

```
ssh-keygen -t ed25519 -C "you@example.com"
cat ~/.ssh/id_ed25519.pub
```

Paste the `cat` output into the form above. The private half (`~/.ssh/id_ed25519`, no `.pub`) stays on your machine; never paste it anywhere.

## Common gotchas

- The PRIVATE key never goes here. Only the `.pub` half.
- ed25519 is recommended; RSA ≥ 3072 also fine. ECDSA accepted but not recommended.
- Set the remote URL on the agent side. Docker image: `ssh://git@<host>:2222/~/<repo>.git` (keep the `~/`). Bare-metal install: `ssh://git@<host>/srv/gateway/repos/<repo>.git` (port 22, full path). Health -> Diagnostics prints the exact URL for each repo.

For depth: [Quick start](https://nimblegate.com/docs/quick-start).
