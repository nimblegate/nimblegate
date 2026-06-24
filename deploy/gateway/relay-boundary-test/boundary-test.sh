#!/usr/bin/env bash
# Adversarial proof of the privilege-separated relay boundary, run as root
# inside a throwaway container (a real multi-user OS). It sets up the two-user
# layout the design calls for and then tries, AS THE git USER, to do the things
# the boundary must forbid - and confirms the legitimate relay path still works.
#
# This is TEST SCAFFOLDING: it configures a real gateway with the real binary
# and asserts behavior. Nothing here is compiled into nimblegate.
set -euo pipefail

NBG=/usr/local/bin/nimblegate
REPOS=/srv/repos
POLICY=/srv/policy
UPSTREAM=/srv/upstream.git
SOCK=/run/nbg/relay.sock
AUTHKEYS=/etc/ssh/git_authorized_keys

ok()   { echo "  ok: $*"; }
fail() { echo "BOUNDARY TEST FAILED: $*" >&2; exit 1; }
asgit() { su -s /bin/sh git -c "$1"; }

echo "== setup: users + shared socket group =="
groupadd -f relaygrp
useradd -r -M -s /usr/sbin/nologin nbg-relay 2>/dev/null || true
useradd -m -s /bin/bash git 2>/dev/null || true
usermod -aG relaygrp git
usermod -aG relaygrp nbg-relay

echo "== setup: upstream owned by nbg-relay (git cannot write it) =="
git init --bare -q "$UPSTREAM"
chown -R nbg-relay:nbg-relay "$UPSTREAM"
chmod -R go-w "$UPSTREAM"

echo "== setup: register the gated repo AS git, point it at the upstream =="
mkdir -p "$REPOS" "$POLICY"
chown git:git "$REPOS" "$POLICY"
asgit "$NBG gateway add --name demo --upstream 'file://$UPSTREAM' --policy-root '$POLICY' --repos-root '$REPOS' --no-import --relay-socket '$SOCK'"
# The relay user must traverse the repo + policy trees to resolve the repo and
# read gateway.toml, and read the bare objects to push. The code is not the
# secret - only the credential is (kept 0600 below). In production this is a
# shared group; here we world-rX the non-secret trees.
chmod a+rX "$REPOS" "$REPOS/_repos" "$POLICY" "$POLICY/demo"
chmod -R a+rX "$REPOS/_repos/demo.git"
# gateway.toml holds non-secret config (upstream URL, gating) that BOTH git and
# the relay user read. The credential is NOT here - it is the separate 0600 file
# below. gateway add writes gateway.toml 0600 (git-only); make it relay-readable.
# (Production: a shared git+relay group, gateway.toml 0640.)
chmod a+r "$POLICY/demo/gateway.toml"

echo "== setup: credential owned by nbg-relay, 0600 (git must not read it) =="
printf 'fake-upstream-token\n' > "$POLICY/demo/credential"
chown nbg-relay:nbg-relay "$POLICY/demo/credential"
chmod 600 "$POLICY/demo/credential"

echo "== setup: confirm 'gateway add --relay-socket' baked the socket into the hook =="
grep -q "NBG_RELAY_SOCKET=\"$SOCK\"" "$REPOS/_repos/demo.git/hooks/post-receive" \
  || fail "generated post-receive hook is missing NBG_RELAY_SOCKET"
ok "generated post-receive hook routes through the relay socket"

echo "== setup: AuthorizedKeysFile root-owned, outside git's home =="
mkdir -p /etc/ssh
printf '# managed by root; the git user cannot edit this\n' > "$AUTHKEYS"
chown root:root "$AUTHKEYS"
chmod 644 "$AUTHKEYS"

echo "== setup: start the relay service as nbg-relay =="
mkdir -p /run/nbg
chown nbg-relay:nbg-relay /run/nbg
su -s /bin/sh nbg-relay -c \
  "$NBG gateway relay-service --policy-root '$POLICY' --repos-root '$REPOS' --socket '$SOCK' --socket-group relaygrp --reconcile-interval 0" \
  >/tmp/relay.log 2>&1 &
for _ in $(seq 1 25); do [ -S "$SOCK" ] && break; sleep 0.2; done
[ -S "$SOCK" ] || { cat /tmp/relay.log; fail "relay socket never appeared"; }

echo
echo "== ASSERTION 1: git cannot READ the credential =="
if asgit "cat '$POLICY/demo/credential'" >/dev/null 2>&1; then
  fail "git read the credential - boundary broken"
fi
ok "git denied reading the credential (nbg-relay:0600)"

echo "== ASSERTION 2: git cannot push DIRECTLY to the upstream =="
WORK=/home/git/work
asgit "rm -rf '$WORK' && git init -q '$WORK' && cd '$WORK' && git config user.email t@t && git config user.name t && echo hi > a && git add . && git commit -qm first"
WANT=$(asgit "cd '$WORK' && git rev-parse HEAD")
if asgit "cd '$WORK' && git push -q 'file://$UPSTREAM' HEAD:refs/heads/main" >/dev/null 2>&1; then
  fail "git pushed directly to the upstream - boundary broken"
fi
ok "git denied direct upstream push"

echo "== ASSERTION 3: push through the gate IS relayed (by nbg-relay) =="
asgit "cd '$WORK' && git push -q '$REPOS/_repos/demo.git' HEAD:refs/heads/main"
GOT=$(git --git-dir "$UPSTREAM" rev-parse refs/heads/main 2>/dev/null || echo none)
[ "$GOT" = "$WANT" ] || { cat /tmp/relay.log; fail "relay did not deliver (got $GOT want $WANT)"; }
ok "relay delivered the push git itself could not ($WANT)"

echo "== ASSERTION 4: git cannot edit authorized_keys =="
if asgit "echo pwned >> '$AUTHKEYS'" 2>/dev/null; then
  fail "git modified authorized_keys - inbound hardening broken"
fi
ok "git denied editing authorized_keys"

echo
echo "ALL BOUNDARY ASSERTIONS PASSED"
