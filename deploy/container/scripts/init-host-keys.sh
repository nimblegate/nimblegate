#!/bin/sh
# Generate SSH host keys (persisted on the ssh volume), configure sshd, and
# sync the dashboard-maintained authorized_keys file to the git user.
# Invoked once at container start by the init-host-keys s6 oneshot.
set -eu

SSH_DIR=/srv/gateway/ssh
mkdir -p "$SSH_DIR/hostkeys"

# Persistent host keys (so dev-side known_hosts trust survives container recreation).
for t in rsa ecdsa ed25519; do
  key="$SSH_DIR/hostkeys/ssh_host_${t}_key"
  if [ ! -f "$key" ]; then
    ssh-keygen -q -t "$t" -f "$key" -N ''
  fi
done

# The dashboard-managed authorized_keys file lives directly on the ssh volume;
# sshd reads it via AuthorizedKeysFile below. Single source of truth - no
# sync between $SSH_DIR/authorized_keys and /home/git/.ssh/authorized_keys.
touch "$SSH_DIR/authorized_keys"
chown -R git:git /srv/gateway/repos /srv/gateway/cfg "$SSH_DIR"
chmod 755 "$SSH_DIR"
chmod 600 "$SSH_DIR/authorized_keys"

# Key-only sshd, git user only, using the persistent host keys + the
# volume-mounted authorized_keys file (managed by the dashboard at /ssh-keys).
#
# Banner directive points sshd at a pre-auth message shown on every
# connection - including failed ones - so a developer hitting
# "Permission denied (publickey)" sees instructions for adding their
# public key BEFORE the failure line. Without this, the only signal
# back to the dev box is the generic OpenSSH rejection with no
# gateway context.
cat > /etc/ssh/sshd_config <<EOF
Port 22
HostKey $SSH_DIR/hostkeys/ssh_host_rsa_key
HostKey $SSH_DIR/hostkeys/ssh_host_ecdsa_key
HostKey $SSH_DIR/hostkeys/ssh_host_ed25519_key
AuthorizedKeysFile $SSH_DIR/authorized_keys
PermitRootLogin no
PasswordAuthentication no
PubkeyAuthentication yes
AllowUsers git
StrictModes yes
X11Forwarding no
AllowTcpForwarding no
AllowAgentForwarding no
AllowStreamLocalForwarding no
PermitTTY no
PermitTunnel no
Banner /etc/ssh/nimblegate-banner
EOF

# Pre-auth banner - sshd sends it before authentication on EVERY connection,
# successful pushes included, and cannot vary it by outcome. So it is two
# lines worded as a conditional hint, naming no key path or address (the
# developer's key file and the dashboard's mapped port are not known here).
cat > /etc/ssh/nimblegate-banner <<'EOF'
nimblegate: if the next line says "Permission denied", add this machine's
public key on the gateway dashboard's SSH Keys page.
EOF
chmod 644 /etc/ssh/nimblegate-banner

# Welcome banner. The setup-token line that follows this banner (printed by
# the dashboard process when no admin user exists) is the load-bearing
# bootstrap signal; the banner just orients the operator on where to go next.
#
# The endpoints shown here are the in-container bindings. Host-side ports are
# set by compose.yaml (the operator's machine controls those; we can't read
# their mapping from inside). If you changed NIMBLEGATE_DASHBOARD_PORT or
# NIMBLEGATE_SSH_PORT in compose, swap them into the URLs below.
printf '==================================================================\n %s - combined gateway\n' "$(/usr/local/bin/nimblegate version 2>/dev/null || echo nimblegate)"
cat <<'EOF'

  In-container endpoints:
    Dashboard:  http://localhost:7900/         (host port: see compose.yaml; default 7900)
    Git push:   git@<host>:<repo>.git          (host port: see compose.yaml; default 2222)
  Installed on a remote/headless host? The dashboard is published on the host's
  loopback only (it's an admin surface). From your laptop, tunnel to it:
    ssh -L 7900:127.0.0.1:7900 <user>@<host>   then open http://localhost:7900
    (use 127.0.0.1, not localhost - it's published on IPv4; localhost may pick ::1)
  If you overrode the host ports via NIMBLEGATE_DASHBOARD_PORT or
  NIMBLEGATE_SSH_PORT in your .env / compose env, use those instead of the
  defaults in the URLs your devs use.

  First-time setup (all in the dashboard):
    1. /setup        - paste the setup token (printed below by [nbg-setup]
                       when no admin user exists yet) and choose your
                       username + password
    2. /ssh-keys     - paste the dev box's SSH public key
                       (on the dev box: cat ~/.ssh/id_ed25519.pub)
    3. /policy       - add a repo: name + upstream URL + token
    4. On the dev box, add an ~/.ssh/config entry for the non-default port:
         Host nimblegate
           Hostname  localhost          # or your gateway host
           Port      2222
           User      git
         Then:
           git remote set-url origin nimblegate:<repo>.git
           git push   # nimblegate gates the push, relays on accept
       (Without an ssh config entry, use:
          GIT_SSH_COMMAND="ssh -p 2222" git push ssh://git@<host>:2222/~/<repo>.git)

  Logs to watch (on the HOST):
    docker logs <container> | grep nbg-setup       - current setup token
    docker logs <container> | grep nbg-supervise   - service restart events

  Operator commands (via: docker exec <container> <cmd>):
    nbg-status                       - uptime, services, repos, keys, auth
    nbg-restart [sshd|dashboard|all] - restart a service (s6 supervised)
    nbg-logs    [target] [N]         - docker-logs recipe cheatsheet
    nbg-reset   --logs|--all|--hard  - clear state (destructive; needs --yes)
    nbg-regen-keys --yes             - rotate SSH host keys (breaks TOFU)
==================================================================
EOF
