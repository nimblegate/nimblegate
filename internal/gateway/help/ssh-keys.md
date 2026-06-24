# SSH keys

Authorized public keys for the gateway. Every `git push` against the gateway runs over SSH; the agent must hold the private key matching one of these public keys.

## Key actions

- **Add a key**: paste the public key (one line, `ssh-ed25519 AAAA... user@host`). It's stored mode 0600 in the container's ssh volume.
- **Delete a key**: removes it from `authorized_keys`; immediate, no grace period. Active sessions stay connected; new pushes need a different key.
- **Name a key**: the optional comment field shows in [Events](/events) so you know which key was used.

## Don't have an SSH key yet?

One command, press Enter at each prompt to accept defaults; pick a passphrase or leave blank:

```
ssh-keygen -t ed25519 -C "you@example.com"
cat ~/.ssh/id_ed25519.pub
```

Paste the `cat` output into the form above. The private half (`~/.ssh/id_ed25519`, no `.pub`) stays on your machine; never paste it anywhere.

## TOFU: Trust On First Use

The first time a key pushes to a repo, the gateway records it as "seen". Subsequent pushes from the same key are familiar (:icon-accept: in feed); pushes from new keys to the same repo show `?` so you notice unfamiliar agents.

## Common gotchas

- The PRIVATE key never goes here. Only the `.pub` half.
- ed25519 is recommended; RSA ≥ 3072 also fine. ECDSA accepted but not recommended.
- Set the remote URL on the agent side: `ssh://git@<host>:2222/<repo>.git`.

For depth: [README: Setup](https://github.com/nimblegate/nimblegate/blob/main/README.md#setup--three-pages-one-each).
