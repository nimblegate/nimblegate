#!/usr/bin/env bash
# Adversarial proof of per-key repo scoping, run as root in a throwaway
# container. It registers two repos, enables scoped access (migrate), grants
# key A only repo1, then runs the EXACT forced command sshd would run (extracted
# from authorized_keys) with $SSH_ORIGINAL_COMMAND set - proving key A reaches
# repo1 but is denied repo2 and any non-git command.
#
# Test scaffolding: real binary, real config. Nothing here is in the binary.
set -euo pipefail

NBG=/usr/local/bin/nimblegate
REPOS=/srv/repos
POLICY=/srv/policy
UP=/srv/up
AUTHKEYS=/home/git/authorized_keys   # in git's home (git-writable, like the real /home/git/.ssh/authorized_keys)

ok()   { echo "  ok: $*"; }
fail() { echo "SCOPED-ACCESS TEST FAILED: $*" >&2; exit 1; }
asgit() { su -s /bin/sh git -c "$1"; }

echo "== setup: git user + two registered repos, each with a commit =="
useradd -m -s /bin/bash git 2>/dev/null || true
mkdir -p "$REPOS" "$POLICY" "$UP"
chown git:git "$REPOS" "$POLICY" "$UP"
for r in repo1 repo2; do
  asgit "git init --bare -q '$UP/$r.git'"
  asgit "$NBG gateway add --name $r --upstream 'file://$UP/$r.git' --policy-root '$POLICY' --repos-root '$REPOS' --no-import"
  W=$(mktemp -d); chown git:git "$W"
  asgit "git init -q '$W' && cd '$W' && git config user.email t@t && git config user.name t && echo hi > a && git add . && git commit -qm c && git push -q '$REPOS/_repos/$r.git' HEAD:refs/heads/main"
done

echo "== setup: enable scoped access (key A → migrate → tighten to repo1 only) =="
ssh-keygen -t ed25519 -N '' -C keyA -f /srv/keyA -q
FPA=$(ssh-keygen -lf /srv/keyA.pub | awk '{print $2}')   # SHA256:…
cp /srv/keyA.pub "$AUTHKEYS"; chown git:git "$AUTHKEYS"
asgit "$NBG gateway access migrate --ssh-authorized-keys '$AUTHKEYS' --policy-root '$POLICY' --repos-root '$REPOS'"
asgit "$NBG gateway access revoke --repo repo2 --key '$FPA' --policy-root '$POLICY'"   # key A: repo1 only

# Extract the EXACT forced command sshd would run for this key.
FORCED=$(sed -n 's/^command="\([^"]*\)".*/\1/p' "$AUTHKEYS" | head -1)
[ -n "$FORCED" ] || fail "authorized_keys has no forced command after migrate"
echo "$FORCED" | grep -q "gateway shell --key $FPA" || fail "forced command does not pin key A's fingerprint"
ok "authorized_keys routes key A through: $FORCED"

# Run the extracted forced command exactly as sshd would: as git, with the git
# request in $SSH_ORIGINAL_COMMAND. upload-pack with /dev/null stdin advertises
# refs then exits non-zero (no client) - so we judge by OUTPUT, written to a
# file (binary-safe grep, exit-code-independent), not the exit code.
run_keyA() { runuser -u git -- env "SSH_ORIGINAL_COMMAND=$1" $FORCED </dev/null >/tmp/out 2>&1 || true; }

echo
echo "== ASSERTION 1: key A reaches its granted repo (repo1) =="
run_keyA "git-upload-pack 'repo1.git'"
grep -qa 'refs/heads/main' /tmp/out || { cat /tmp/out; fail "granted repo1 should advertise refs/heads/main"; }
ok "key A fetches granted repo1 (ref advertised)"

# Denials are camouflaged: the client sees a vanilla "Repository not found",
# never "access denied" / the real reason (which goes to the operator audit).
echo "== ASSERTION 2: key A is DENIED a repo it wasn't granted (repo2) =="
run_keyA "git-upload-pack 'repo2.git'"
grep -qa 'Repository not found' /tmp/out || { cat /tmp/out; fail "repo2 denial should read 'Repository not found'"; }
grep -qa 'access denied' /tmp/out && { cat /tmp/out; fail "denial must NOT leak the real reason to the client"; }
ok "key A denied repo2 (vanilla 'Repository not found')"

echo "== ASSERTION 3: key A cannot push repo2 either =="
run_keyA "git-receive-pack 'repo2.git'"
grep -qa 'Repository not found' /tmp/out || { cat /tmp/out; fail "push to repo2 should be denied"; }
ok "key A denied push to repo2"

echo "== ASSERTION 4: a non-git command is refused =="
run_keyA "rm -rf /"
grep -qa 'Repository not found' /tmp/out || { cat /tmp/out; fail "non-git command should be refused opaquely"; }
ok "non-git command refused (opaque)"

echo
echo "ALL SCOPED-ACCESS ASSERTIONS PASSED"
