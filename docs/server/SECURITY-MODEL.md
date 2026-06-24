# nimblegate: security model and hardening

How the gate keeps agent / human pushes from compromising your gateway server, what it actually protects against, where the boundaries are, and how to tighten them. Read this before changing SSH keys, the git user's shell, the hooks, or how the binary runs.

---

## TL;DR

nimblegate sits as an SSH-accessible bare git repo with policy hooks. An attacker (or your AI agent) can push code through the gateway, but **cannot run arbitrary commands on the gateway box**, **cannot bypass the policy frames**, and **cannot tamper with policy config via the push itself**. The protection rests on four layers stacked one inside the other:

1. **SSH key auth** to a non-privileged `git` user
2. **`git-shell`** as the login shell (rejects everything except `git-receive-pack` / `git-upload-pack` / `git-upload-archive`)
3. **Git itself** (well-audited, kept current via your distro's security updates)
4. **The pre-receive hook**: your policy frames materialize the push into a temp tree, run rules, accept or reject

Defense in depth: a hole in any one layer is contained by the next.

---

## Threat model

### What nimblegate is designed to stop

- **An AI agent committing leaks**: credentials, private keys, force-push to main, `rm -rf` of protected paths, `curl | sh` install scripts, schema drifts that crash prod, Trojan Source attacks pasted from chat, etc. The policy frames are the catalog of what it catches.
- **An attacker who steals a git push credential** from being able to push *anything*. The gate still runs against their pushes, same as yours.
- **A misconfigured push** from going to your real upstream unreviewed. Observe mode lets you turn this on softly before you turn on enforcement.

### What nimblegate is NOT designed to stop

- **Someone with shell access to your gateway box.** Once they're on the host, the gate is irrelevant; they can edit policy, replace the binary, or push directly to upstream around the gateway. **Protect SSH access to the gateway HOST aggressively.** (The `git` user is not that; see boundaries below.)
- **A compromised upstream credential.** If your gitea PAT leaks, an attacker can push to gitea directly, bypassing the gate. Rotate PATs you suspect, keep them scoped narrowly (`write:repository` is enough).
- **A vulnerability in git itself.** Keep git updated. nimblegate inherits git's security posture.
- **Bugs in the policy frames or materialization code.** These are your code. The frames are open-source markdown + Go; review the ones you depend on for catastrophic-prevention.
- **An agent that can reach upstream directly from the dev machine.** If the box your agent runs on holds an upstream credential (PAT in `~/.git-credentials`, gh CLI token, pubkey registered on GitHub) the agent can push around the gate entirely. Closing this is **dev-machine hygiene**: keep the dev machine's only credential pointed at the gateway. See [`DEV-MACHINE-SETUP.md`](DEV-MACHINE-SETUP.md).
- **An agent that actively fingerprints its environment.** A determined agent with host access can detect the gate regardless of how you name things: by sniffing the SSH port, noticing a push that succeeds locally but never appears on the public upstream, finding the dashboard on the LAN, or reading the box's process list. Naming hygiene (below) defeats *incidental* discovery, not a probe. If the agent has shell on the dev box, you are in the "shell access" non-goal above.

---

### Keeping the gate invisible to a gated agent (observer effect)

This is a soft property, not a boundary, but it is what makes **observe mode** worth running. The point of observe mode is to watch what an agent does when it believes nothing is stopping it. An agent that can tell it is behind a guardrail will push clean while watched and revert when it thinks it is not (eval-awareness / sandbagging). This has been seen in practice. So observe mode is only as honest as the gate is unrecognizable.

**The gate's own wire behaviour is already silent by design** (`internal/gateway/prereceive.go`):

- An **observe-mode would-block relays silently** - `return 0`, no output to the pushing client. A gated push is byte-for-byte indistinguishable from one with no gateway in front.
- An **enforce-mode reject is unbranded** - `error: push rejected by repository policy:`, with no "gateway", "nimblegate", or "relay" language. It reads like any ordinary git host's pre-receive decline.
- The **notification rail is unnamed** for the same reason.

**What the gate cannot suppress is the client-side remote URL.** `git push` always echoes the configured remote on its own `To …` line - that is git on the pusher's machine, not the gate. So invisibility is **a property of the remote URL you hand each consumer, not an automatic guarantee:**

- **Operators** (a human on the dev box, the deploy scripts) may use the explicit form - full path, descriptive SSH alias - because they are *supposed* to know the gate exists.
- **A gated agent must only ever see the neutral short form:**

  ```
  origin = git@<neutral-host>:<repo>.git
  ```

  - **No `/srv/gateway/repos/` path** - the short URL works because the `git` user's home *is* the activation root, so the prefix never appears in what the agent sees.
  - **No "gate" / "nimblegate" / "nbg" in the hostname or SSH alias** - pick a name a plain git host would have (`git.internal`, etc.). The alias `nbg:` or a host called `gateway.lan` is itself the tell.
  - The agent's `To …` line then reads `To git@git.internal:repo.git` - nothing that names a gate.

  > **Counter-example:** this repo's own `gateway` remote is `nbg:/srv/gateway/repos/nimblegate.git` - descriptive alias *and* full path. That is correct for an operator, but it is exactly the config that would betray observe mode if an agent inherited it. Never point a gated agent at a remote configured this way.

**Same discipline for the working tree.** Keep observe-mode policy **gateway-side**, never in the agent's repo. If `appframes.toml`, `.appframes-ignore`, frame `.md` files, or a `CLAUDE.md` mentioning the gate sit in the tree the agent reads, it learns the gate exists from the first `ls` - the URL hygiene above is wasted. (This is also the validity condition for any agent-behaviour benchmark: the corpus repos must be gate-free in content, or you are measuring agents that know they are measured.)

**Honest limit.** All of the above defeats *incidental* discovery by an agent that is not hunting for a gate. It does not defeat an agent with host access or one that actively probes (see the last NOT-designed-to-stop bullet). Treat naming hygiene as raising the cost of discovery, not as a guarantee of invisibility.

---

## The four security boundaries

### Layer 1: SSH key authentication

The gateway's `git` user accepts only SSH key auth (no password). Keys are listed in `/srv/gateway/ssh/authorized_keys`, managed via the dashboard's `/ssh-keys` page.

**What this layer enforces:** only holders of an authorized private key can connect at all.

**What this layer does NOT enforce:** which user is behind that key. A key shared between you and your AI agent (same `~/.ssh/id_ed25519` on the same dev machine) authenticates as the same identity. If you want a stricter separation, see hardening below.

### Layer 2: `git-shell`

The `git` user's login shell is `/usr/bin/git-shell` (verify: `getent passwd git | cut -d: -f7`). Git-shell is a restricted shell built into git that accepts ONLY these commands from SSH:

- `git-receive-pack '<repo>'`
- `git-upload-pack '<repo>'`
- `git-upload-archive '<repo>'`
- Anything in `~git/.git-shell-commands/` (an optional directory; **on your gateway this directory does not exist**, so this surface is empty)

Anything else (`ssh git@host bash`, `ssh git@host ls`, `ssh git@host 'curl ... | bash'`) gets `fatal: Interactive git shell is not enabled.` and the connection drops.

**Verify the boundary:** (from a box with an authorized key)

```bash
ssh git@<gateway> bash       # should error
ssh git@<gateway> 'echo hi'  # should error
ssh git@<gateway> ls         # should error
```

If any of those print output, the configuration is wrong: investigate `authorized_keys` for `command=` overrides and `~git/.git-shell-commands/`.

**What this layer enforces:** a key holder can push and pull git refs, nothing else.

### Layer 3: git itself

Git's `receive-pack` parses the incoming pack file, validates objects, and updates refs. Git has had server-side CVEs over the years (.gitmodules injection, submodule URL parsing, gitattributes overflows). Current versions are hardened against the known ones.

**Verify your version:**

```bash
ssh root@<gateway> 'git --version'
```

Anything 2.40+ on Debian/Ubuntu with current security updates is fine. 2.30 or older has known unpatched issues, so upgrade.

**Verify auto-updates are on (Debian/Ubuntu):**

```bash
ssh root@<gateway> 'systemctl is-enabled unattended-upgrades 2>/dev/null; dpkg -l unattended-upgrades 2>/dev/null | tail -1'
```

If `unattended-upgrades` isn't installed and enabled, security patches don't land until you manually `apt upgrade`. Recommended: install + enable, restrict to security updates only.

### Layer 4: nimblegate's pre-receive policy hook

When a push arrives, `/srv/gateway/repos/<repo>.git/hooks/pre-receive` runs (as the `git` user) and exec's `/usr/local/bin/nimblegate gateway pre-receive`. That command:

1. **Materializes** the pushed tree into a fresh `/tmp/afgw-<random>/` using `git archive | tar -x` (not `git checkout`, so `.gitattributes` filters, smudge filters, and `post-checkout` hooks DO NOT run during evaluation, deliberately)
2. **Overlays the gateway-held policy** into the temp tree (strips the push's own `appframes.toml`, `.appframes/`, and `.appframes-ignore` markers; copies the gateway's policy in their place). The push CANNOT shadow or downgrade the policy by including its own config.
3. **Runs every enabled frame** against the temp tree
4. **Applies the whitelist** (also overlaid from the gateway policy dir, not the push)
5. **Accepts or rejects** the push based on the result, plus optional **observe mode** which logs would-have-rejected pushes but accepts them anyway

**What this layer enforces:** the policy you've configured, on every push, deterministically. No exceptions, no time-of-check-time-of-use races (the materialized tree is what gets evaluated AND relayed; they're the same bytes).

**What this layer does NOT enforce:** the correctness of YOUR policy frames. If a frame has a bug that lets a malicious pattern through, the bug is in the frame.

---

## Concrete walk-through: what happens when an agent pushes

A real scenario from a recent session, what is and isn't protected when an AI agent connected to your dev machine runs `git push origin main`:

1. The agent uses `~/.ssh/id_ed25519` (the same key the human uses) → **Layer 1 passes**: the key is authorized.
2. The SSH session lands in `git-shell` → **Layer 2 holds**: the agent can ONLY run git protocol commands. It cannot get arbitrary shell on the gateway.
3. `git-receive-pack` parses the incoming pack → **Layer 3 holds** assuming git is patched.
4. `pre-receive` runs all the policy frames against the pushed code → **Layer 4 catches** anything the policy is configured to catch (credentials, force-push, the encoding attack class, etc.).

**Result:** the agent can push *clean* code that passes policy. It CANNOT push code containing leaked credentials, attack payloads, or other policy-blocked patterns. It CANNOT read other repos' credentials, edit gateway config, or run commands on the gateway host.

That's the protection you have. The fact that the agent has push capability AT ALL is the architectural reality of nimblegate: the whole point is that an agent can push *and the gate filters what's allowed through*. It's not a bug, it's the design.

---

## Privilege-separated upstream relay

The gate is only un-bypassable if the **upstream credential lives where the push-facing user can't read it**. By default the relay runs *inline in the `post-receive` hook, as the `git` user*, so the `git` user (which receives every dev/agent push) can read the credential. The boundary then rests entirely on "`git-shell` holds + the box is separate": a single `git-shell` escape or a subverted hook would let an attacker lift the credential and push straight to the upstream, around the gate. That's a thin boundary for a tool that sits in your push path and holds your upstream credential.

**The privilege-separated relay closes it.** Opt in with `gateway add --relay-socket <path>` + the `nimblegate-relay.service` unit, and:

- The **credential is owned by a dedicated `nbg-relay` user, mode `0600`**, so the `git` user cannot read it. Run the relay as `nbg-relay` (the systemd unit does this).
- The `post-receive` hook (as `git`) holds **no credential**; it hands a **credential-free `(repo, refs)` job** to the relay service over a **local Unix socket** (`0660`, group-restricted to the `git` user) and blocks for the result, so the pusher still sees an immediate `relay FAILED`.
- The relay service **validates every job against the gated bare repo**: it only relays a `ref@rev` whose object matches the repo's *current* state. A forged or replayed job cannot make the credential-holding service push un-gated content; the content must already be in the gated repo to be pushable.
- A **reconciler** re-pushes any ref the upstream is missing or behind (recovers pushes accepted while the relay was down, automating the old "operator must re-run relay").

With this on, an attacker who gets the `git` user (a `git-shell` escape, a subverted hook) gets **nothing reusable**: no credential, no way to reach the upstream directly.

Two rules the separated model **requires** (enforced/warned by `gateway add`):

1. **The credential lives ONLY in `<policy-root>/<repo>/credential`** (`nbg-relay:0600`), **never** as a token in the `upstream-url`. `gateway.toml` is git-readable (the `git` user needs the gating config in `pre-receive`), so a token in the URL would be readable by exactly the user the boundary excludes. `gateway add --relay-socket` warns if the upstream URL embeds a token.
2. **`gateway.toml` must be relay-readable** (e.g. a shared `git`+`nbg-relay` group, `0640`): it holds the *non-secret* upstream URL + gating config that both users read.

Pair this with the inbound hardening (`AuthorizedKeysFile` pointed at a **root-owned** file outside the `git` user's home, so a compromised `git` user can't add its own keys) and the boundary holds in software, not just by convention. See [`README.md`](README.md) "Deploy the privilege-separated relay" for the runbook.

---

## Per-key repo scoping (optional)

By default, any key in `authorized_keys` can clone/push **every** repo the git user can reach: `git-shell` restricts *what* you can run (git only), not *which repos*. That's fine for a single-tenant gateway; for multiple devs at different trust levels it's a gap.

**Scoped access** closes it with the Gitolite-style **forced command**. Turn it on with the dashboard's `--scoped-access` and the one-time `nimblegate gateway access-migrate`. Then each `authorized_keys` entry is written as:

```
command="nimblegate gateway shell --key <fp> --policy-root … --repos-root …",restrict ssh-ed25519 AAAA… comment
```

sshd runs *only* that command and passes the client's real git command in `$SSH_ORIGINAL_COMMAND`. `gateway shell` then composes three guards before it will exec git:

1. **Parse + verb whitelist**: only `git-upload-pack` / `git-receive-pack` / `git-upload-archive`; anything else is refused.
2. **Symlink-safe, root-confined repo resolution**: the requested repo name resolves through the activation symlink and must stay inside the repos root (no traversal / symlink escape).
3. **Per-key ACL**: the key's fingerprint must be granted the repo, at the right level (push needs a `write` grant; fetch accepts `read` or `write`). **No grant → denied.**

So a scoped key reaches exactly the repos you grant it and nothing else, proven adversarially in `deploy/gateway/scoped-access-test/` (key A reaches its repo, is denied another for both fetch and push, and any non-git command is refused).

> **Enabling scoped access requires a non-`git-shell` login shell.** sshd runs a forced command *through the git user's login shell*, and `git-shell` rejects anything that isn't a git verb, so the forced command only runs if the git user's shell is a normal shell: `usermod -s /bin/sh git`. That is why scoped access is an advanced opt-in, not the default. Once the shell is `/bin/sh`, the forced command + `restrict` on **every** key is the whole inbound boundary (`git-shell` no longer backstops it), so confirm no plain, forced-command-less line exists in `authorized_keys` - a plain line on `/bin/sh` would grant a full shell. The default single-tenant gateway keeps `git-shell` and does not enable scoped access; the clean `ssh://host/repo.git` URL works only in this scoped mode, so single-tenant gateways push via the `ssh://host:2222/~/repo.git` path form.

**Manage grants** with `nimblegate gateway access grant|revoke|list` or the dashboard's SSH Keys page (scoped section). `access-migrate` seeds every existing key with access to every current repo (preserving today's behavior), which you then tighten. The credential separation (relay) and the inbound `restrict`/`AuthorizedKeysFile` hardening compose with this: scoping is the *which-repos* layer on top of *what-you-can-run*.

---

## Identity preservation through the gateway

The gateway is a relay, not an editor. Commits travel byte-for-byte from your dev machine → gateway → upstream. This is a deliberate property worth understanding because it shapes what you see in the upstream's UI.

**What stays the same end-to-end:**

- **Commit SHA.** The gateway materializes the pushed tree for evaluation, but relays the original pack bytes. The SHA you see locally is the SHA the gateway records is the SHA the upstream stores. Same bytes the whole way through. (This is what makes the dashboard feed's per-row short SHA cross-referenceable with gitea / github / gitlab.)
- **Author and committer.** Whatever your `git config user.name` / `user.email` says when you commit is what shows on every git host's commit view. The gateway does not rewrite either field.
- **GPG / SSH signatures.** Signatures verify end-to-end because nothing in the commit object changes. A signed commit pushed through the gateway stays a verified-signed commit at the upstream.

**What gets added separately (NOT in the commit):**

- **Pusher.** The upstream sees the gateway's credential (the PAT user) as the pusher. Visible on the upstream's Activity tab, push events, and webhook payloads. NOT on the commit list, where your author name still appears.

**Why this matters:** the human who authored the code and the credential that delivered it are different concerns. Conflating them would break `git blame`, break signature verification, and make the upstream's commit view lie about who wrote the code. The gateway's job is to gate delivery, not to claim authorship. If the upstream shows your name on a commit, that's correct: you wrote it. The gateway recording itself as pusher in the Activity log is also correct: it delivered it.

**Common confusion:** "Why does gitea show my name on commits when the gateway is in between?" Because the gateway preserved your name. Check gitea's Activity tab: the PAT user shows there as pusher.

---

## Notification rail security

When `[notification]` is enabled per repo (post-launch optional feature), the gateway adds two outbound channels on a rejected push: a PR comment to the upstream + a webhook POST to a configured URL. Both carry the same JSON payload. This expands the gateway's outbound surface, worth understanding what an attacker could see or do.

### What the payload contains

- The rejected push's ref names, old/new SHAs, pusher's SSH key fingerprint
- Frame IDs that fired (e.g. `security/no-private-keys-in-repo`)
- File paths + line numbers from finding messages
- The operator-configured bot mention + auto-tagged human assignees from the PR

It does NOT contain: source code, repo contents, the credential used to push, or the upstream PAT. The webhook receiver sees what the audit log already records, formatted for machine consumption.

### Webhook secret handling

The webhook auth secret lives at `<policy-root>/<repo>/gateway.toml` (mode 0600 like the upstream credential). It's never logged, never echoed in events, masked in the dashboard's reveal control until the operator opts in to view it. HMAC mode is recommended over Bearer because HMAC verifies the request body's integrity too, not just authenticity: a tampered payload fails verification.

### HMAC vs Bearer trade-off

- **HMAC**: the receiver verifies `X-Nimblegate-Signature: sha256=<hex>` against `HMAC-SHA256(secret, body)`. An attacker who intercepts a signature can't replay it against a different payload, can't tamper with the payload undetected, and can't forge a signature without the secret. Recommended for any production deployment.
- **Bearer**: the receiver checks `Authorization: Bearer <token>` against a known value. Simpler verification (one string compare) but no payload integrity. Acceptable when the receiver is reachable only over a trusted network (LAN, private VPN) and the token leaking would be no worse than the LAN already being trusted.
- **None**: no auth header. Acceptable on a trusted internal network where adding auth is friction without security benefit (e.g. a local-only dashboard receiver).

### What an attacker intercepting a webhook payload sees

- The rejection signal (which frame, which file:line)
- That a particular agent / human was mentioned
- Bot rotation state if multi-bot rotation is enabled

What they DON'T see: source code, the gateway's upstream PAT, other tenants' data (the rail is per-repo). The information disclosure surface is roughly equivalent to the audit log: sensitive enough to warrant HMAC, not sensitive enough to require TLS pinning or mTLS for typical deployments.

### Recommended posture

- HMAC for any webhook URL that crosses a trust boundary
- TLS (HTTPS) for the webhook URL: the gateway sends the payload over whatever scheme the URL specifies; `http://` sends in cleartext
- Rotate webhook secrets quarterly (same cadence as upstream PATs); use the dashboard's "Generate random" button to produce a 256-bit secret
- Monitor the dashboard `/health` page for unexpected webhook URL changes (would show up as new deadletter accumulations against the old URL)
- Don't enable `observe_pr_comments` in production-tuned repos; it's for tuning rules, not for ongoing operation; the inevitable WARN-noise on PR threads becomes ignored

---

## Frame execution limits

Every shipped frame's regex is compiled via Go's `regexp` package (RE2), which guarantees linear-time matching with no backtracking. ReDoS-by-catastrophic-pattern (the `(a+)+` family that wedges PCRE/JavaScript engines) is structurally impossible here, verified by enumeration over the 85 `regexp.MustCompile` sites in the codebase and confirmed at the language-spec level.

What's possible WITHOUT the size cap below: a malicious push containing one multi-GB file matched by a frame's glob would consume RAM proportional to file size during `os.ReadFile`. RE2's O(n) is in bytes; n is unbounded if the frame slurps the whole file. To close this, every frame that reads file content uses `ReadFileBounded` (helper at `internal/checks/checkcommon.go`) with a default 1 MiB cap. The `TestNoUnboundedReadInChecks` regression test fails any future frame that omits the cap, so new frames inherit the protection automatically rather than depending on each author remembering. See `docs/frame-authoring.md` "Reading file content" for the author-facing rule.

The operator-supplied regex path (`/policy/check/preview` + custom linters) is independently bounded by `bufio.Scanner` with a 1 MiB line buffer in `internal/linters/regexscan.go`: per-line streaming caps both regex execution AND file memory.

---

## Push size cap (`receive.maxInputSize`)

Every bare repo nimblegate creates gets `git config receive.maxInputSize 500m` applied at registration time. This is the load-bearing knob against the disk-fill DoS class: an attacker with a valid SSH key (or a compromised dev box) could otherwise push an arbitrarily large pack, which `git-receive-pack` would write to `<repo>.git/objects/incoming-*` BEFORE the pre-receive hook runs. 500 MiB is generous for normal code repositories (typical pushes are a few MB), fits inside every major upstream's own cap (GitHub 2 GiB practical, GitLab 5 GiB default, Gitea unlimited), and bounds a single attacker's disk impact on the gateway box.

Override per repo by editing `<policy-root>/<repo>/gateway.toml`:

```toml
max-input-size = "1g"   # or "0" for unlimited
```

Then apply manually: `git -C /srv/gateway/repos/<repo>.git config receive.maxInputSize 1g`. (A future enhancement could have the maintenance loop re-apply this from policy automatically; today it's set once at registration.)

### Git LFS interaction

Repos with lots of binaries (images, videos, design files) should use Git LFS instead of raising this cap. LFS replaces binary files in git with tiny text pointer files; the actual binary uploads go directly from the developer to the LFS server (your upstream's LFS endpoint, or an S3 backend), bypassing git's normal pack mechanism entirely.

This has a real consequence for nimblegate's threat model: **LFS-stored files bypass the gateway entirely.** The git pack arriving at the gateway only contains pointer files; the binaries are never scanned by the policy frames, never checked for embedded credentials, never gated. Three reasonable postures depending on your trust model:

1. **Allow LFS, accept the gap**: gating scope is code + text only. Fine if you trust your devs and agents not to embed secrets in binaries.
2. **Disable LFS at the upstream**: forces all data through the gateway (Gitea: `[server] LFS_START_SERVER = false`). Everything gateable, but you cap your binary tolerance at whatever `receive.maxInputSize` allows.
3. **LFS-aware gating**: would require nimblegate to proxy LFS uploads too. Significant work; only worth building if a real operator surfaces the gap.

Default posture is (1): LFS allowed, accepted as out-of-gating-scope. Operators with stricter requirements can flip the gitea config (2).

---

## Dashboard bind address

The shipped `deploy/gateway/nimblegate-dashboard.service` defaults to `--addr 127.0.0.1` (loopback only). Reach the dashboard via SSH port-forward (`ssh -L 7900:127.0.0.1:7900 <gateway>`) and open `http://localhost:7900` on your laptop. This is the safest default: even if someone reaches your gateway's port 7900 from the network, the kernel refuses the connection because nothing's listening there.

When you want broader exposure (LAN trust, multi-operator access via nginx + TLS), use `nimblegate gateway bind` rather than hand-editing the unit file. The command takes either a friendly alias or a literal IP:

```sh
nimblegate gateway bind localhost     # back to loopback
nimblegate gateway bind all           # 0.0.0.0 - every interface
nimblegate gateway bind 192.0.2.10  # specific IP
```

It rewrites the systemd unit's `ExecStart` in place, runs `systemctl daemon-reload`, and prints the restart command. Idempotent: re-running with the same arg is a no-op. Pass `--no-reload` to skip the daemon-reload step (rare, useful for batch reconfiguration).

**Public-internet deployment posture:** never `bind all` on an internet-facing host without a reverse proxy enforcing TLS + auth + rate limiting in front of port 7900. The dashboard's single-admin bcrypt auth is sufficient for LAN/VPN deployments but accepts unlimited connection attempts; a public exposure should land behind a TLS-terminating proxy that does its own rate-limit + IP allowlist as a first filter.

**One-command public deployment via Caddy:** for the public-deployment path, run `nimblegate gateway tls-setup --domain <your-domain>` on the gateway box. The command (a) confirms Caddy is installed, (b) writes a minimal Caddyfile reverse-proxying to `127.0.0.1:7900` with sane security headers (HSTS, X-Content-Type-Options, X-Frame-Options, Referrer-Policy), (c) flips the dashboard's bind to localhost via `gateway bind localhost`, (d) restarts both services. Caddy provisions the TLS cert via Let's Encrypt on first startup. Requirements: DNS A/AAAA record pointing at the gateway box, ports 80 + 443 reachable. Pass `--dry-run` to see what would change without touching anything. The reference Caddyfile template lives at `deploy/gateway/Caddyfile.template` for operators who prefer to manage Caddy config by hand.

Caddy beats nginx for the appliance use case because auto-HTTPS via Let's Encrypt removes the cert-management burden and the Caddyfile syntax is small enough to ship as a template. nginx still works if your environment already has it: the same posture rules apply (loopback bind, security headers, TLS termination).

---

## Maintenance loop: what it touches

The gateway daemon runs a background goroutine that periodically calls `git gc --auto --quiet` against each bare repo under `--repos-root`. Default interval is 168h (weekly); configurable in `<policy-root>/gateway.toml` under `[maintenance]`. The loop touches only objects inside `<repos-root>/<repo>.git/objects/`: no new network exposure, no new permission requirements (it runs as whatever user the daemon runs as, which already has read+write on the bare repos). Disabled by setting `enabled = false` in the config, or by not configuring `--repos-root` at all. Activity is logged to the existing audit/event log so operators can see what was compacted and when.

The loop is intentionally NOT aggressive: `--prune=now` and `--aggressive` are not configurable. Aggressive pruning would delete unreachable objects still useful as delta-bases for incremental pushes; the conservative `--auto` mode self-throttles and respects git's standard 2-week prune grace period. If you ever need to scrub bytes (e.g., a secret reached the bare repo before the credentials frame caught it), run the manual incident recipe in the "What if a secret reaches the bare repo" section below, not via the maintenance loop.

---

## Dashboard preview endpoint: what it exposes

The dashboard's authoring panel includes a regex-preview button (`/policy/check/preview`) that lets the operator test a candidate check before saving it. It does this by materializing the bare repo's HEAD tree into a temporary directory and running the operator-supplied regex against files matching operator-supplied glob patterns; it returns up to 20 file:line + matched-label HTML snippets. This is the one dashboard surface that touches actual repo content (everything else operates on policy + audit metadata).

**What's reachable:**
- HEAD tree content of registered repos only: no branches, no commit history, no non-HEAD blobs
- Matches are HTML-escaped before write; the matched bytes themselves are visible to the operator

**What guards it:**
- Mounted only when the daemon is started with `-allow-edits`; without that flag the route doesn't exist on the mux
- CSRF token + same-origin check on every request (`guarded()` in `gatewayauthoring.go`)
- The dashboard's auth middleware: default `-auth setup-token` requires a valid admin session cookie

### Why this is safe under v0.1.0's single-admin auth

The dashboard's auth model is intentionally single-admin: one bcrypt credential, one session, no roles. Anyone holding that credential already has the credentials they'd need to SSH into the gateway box and `cat` the bare repos directly. The preview endpoint just gives the same person a regex grep instead of a tar file: it's not a privilege escalation, it's a workflow convenience.

### Why this changes the moment multi-user / RBAC auth ships

When the commercial-tier multi-user auth lands (parked in `_future.md` → "Gateway dashboard auth (SSO/RBAC)"), the preview endpoint becomes a privilege-escalation vector: a read-only operator would be able to grep HEAD content of any repo their session can name, dumping secrets / private code that the RBAC layer was meant to restrict.

Multi-user MUST NOT ship without three additions to this endpoint:
1. **Per-repo capability check**: `canPreview(session, repo)` consulted before `gateway.PreviewTree(bare)`; returns 403 if the session role doesn't include preview on this repo
2. **Hard result-size cap**: currently 20 hits are *displayed*, but the underlying scan runs the full tree; cap the internal hit count to prevent low-and-slow exfil
3. **Audit-log entry per call**: record `(session, repo, regex, pattern count, hit count, timestamp)` so operators can audit who grepped what

This requirement is tracked in `_future.md` → "Dashboard preview endpoint: RBAC + audit hardening" and is **release-gating for multi-user**, not a follow-on.

---

## Hardening: tightening from the default

The default install is solid for a small/personal setup. These changes tighten it further as your trust requirements grow.

### Easy (no operational pain)

1. **Verify `~git/.git-shell-commands/` does not exist.**

   ```bash
   ssh root@<gateway> 'ls -la ~git/.git-shell-commands/ 2>&1 | head -3'
   ```

   Expected: `No such file or directory`. If the directory exists with executables, ANY authorized SSH key can run those executables remotely, a deliberate git-shell feature that you almost certainly don't want.

2. **Pin git auto-updates** (Debian/Ubuntu):

   ```bash
   ssh root@<gateway> '
     apt install -y unattended-upgrades
     systemctl enable --now unattended-upgrades
   '
   ```

3. **Restrict SSH key comments.** When you authorize a key, the comment (`alice@host`) appears in the audit log. Use comments that identify the *machine + user*, not the *role*. A leaked key with comment `gateway-admin` tells an attacker which key to target.

4. **Rotate gitea upstream PATs quarterly.** Use the dashboard's "Rotate upstream credential" flow. Old PATs in gitea should be deleted after rotation, not left as dormant.

### Medium (small operational pain)

5. **Close the dev-machine bypass paths.** The gateway is only a boundary if the box your agent runs on cannot reach upstream directly. Three minimum checks per dev machine:

   ```bash
   # 1. No HTTPS credential cache pointing at upstream
   test ! -s ~/.git-credentials && echo "OK: no creds file" || cat ~/.git-credentials  # any output = bypass

   # 2. Dev machine's SSH key not registered upstream
   ssh -o BatchMode=yes -T git@github.com 2>&1 | grep -q "Permission denied" && echo "OK: github SSH refused"
   ssh -o BatchMode=yes -T git@gitlab.com 2>&1 | grep -q "Permission denied" && echo "OK: gitlab SSH refused"

   # 3. All project remotes point at the gateway, not upstream
   find ~ /media -maxdepth 5 -name "config" -path "*/.git/*" 2>/dev/null \
     | xargs grep -lE "url = .*(github\.com|gitlab\.com|<your-gitea-host>)" 2>/dev/null
   # any output = a project that bypasses the gate; re-point its origin
   ```

   Full setup, retrofit recipe, and credential-store sweep (macOS keychain, libsecret, gh / glab / tea CLIs, gpg-encrypted stores) in [`DEV-MACHINE-SETUP.md`](DEV-MACHINE-SETUP.md).

6. **Use a passphrase-protected SSH key for human pushes; don't load it into ssh-agent.** This is the simplest way to keep an AI agent on the same dev machine from being able to push to the *gateway* autonomously (distinct from the dev-machine bypass paths above, which are about reaching the *upstream* directly).

   On your dev machine:

   ```bash
   # Create a separate key that the agent shell can't use.
   ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519_nimblegate -C "human@host"
   # Add a strong passphrase when prompted.
   ```

   Edit `~/.ssh/config` to use that key for the gateway:

   ```
   Host gw-gitea
     Hostname 192.0.2.10
     User git
     IdentityFile ~/.ssh/id_ed25519_nimblegate
     IdentitiesOnly yes
   ```

   In the dashboard `/ssh-keys`, authorize `~/.ssh/id_ed25519_nimblegate.pub` and **revoke** the original `~/.ssh/id_ed25519` (the one the agent can use without a prompt).

   Then in each repo's git config:

   ```bash
   git remote set-url origin git@gw-gitea:nimblegate.git   # uses the new alias
   ```

   When you `git push origin main`, you'll be prompted for the passphrase. The AI agent can't type it. Pushes from the agent fail with "permission denied (publickey)".

   **Trade-off:** you type the passphrase every push (or `ssh-add -t 5m ~/.ssh/id_ed25519_nimblegate` for a 5-minute window).

7. **Move the gateway to the combined container model.** Currently it runs as a bare binary + systemd unit on the host, alongside everything else (sshd, journald, your other repos). The `nimblegate:eval-alpine` container isolates the gate to its own filesystem namespace + network: a compromised frame or materialization bug stays bounded to the container's volumes. Your existing `/srv/gateway/repos`, `/srv/gateway/cfg`, `/srv/gateway/ssh` can be bind-mounted; state survives the migration.

   Trade-off: one-time migration work, slightly more moving parts (Docker layer).

8. **Restrict the dashboard to localhost-only + reverse-proxy with auth in front.** Currently `nimblegate-dashboard.service` binds `0.0.0.0:7900` (anyone on your LAN can reach it). Bind to `127.0.0.1` and front it with caddy / nginx + an `Authorization` requirement. The compose.yaml in this repo shows this pattern.

### Hard (real operational pain, real security gain)

9. **Hardware-token (yubikey) git push auth.** Use `ssh-keygen -t ed25519-sk` to create a key that requires physical-presence touch on the yubikey. An AI agent CANNOT push without your finger on the device. Authorize the `.pub` half in the dashboard, revoke other keys.

10. **Containerize PLUS run the policy gate as a separate uid from the dashboard.** The dashboard is read-mostly (reads policy / writes audit). The pre-receive hook is the actually-runs-untrusted-input layer. Splitting them across users means a hook bug doesn't compromise dashboard state.

11. **Move gitea upstream URLs to HTTPS or SSH.** http:// for LAN gitea is fine for trust-the-network setups but the relay credential travels in cleartext between gateway and gitea. Adding TLS or switching to SSH eliminates that exposure window.

---

## Where the vulnerability surfaces are: things to NOT touch lightly

These changes weaken the model. Make sure you know what you're trading.

### Don't change the `git` user's shell

```bash
# DON'T:
chsh -s /bin/bash git
```

This turns off Layer 2 entirely. Any authorized SSH key can now `ssh git@host bash` and run arbitrary commands. Don't do this even temporarily for "debugging".

### Don't add `command=` overrides to `authorized_keys`

The `authorized_keys` file supports `command="..."` entries that override the user's login shell. Some setups use this to wrap git operations. If you add a `command=`, you bypass git-shell's command-validation: anything in that command runs as the `git` user on every SSH connection. Only use this if you fully understand the implications.

### Don't populate `~git/.git-shell-commands/`

Putting executables here makes them remotely invokable by ANY authorized key. Specifically AVOID putting anything called `no-interactive-login` or anything that does network operations.

### Don't run the dashboard as root

Currently it runs as the `git` user (see `/etc/systemd/system/nimblegate-dashboard.service`, `User=git`). If you change this to `User=root` for "convenience", a bug in the dashboard's auth gate becomes an immediate root compromise. Keep `User=git`.

### Don't disable `unattended-upgrades`

Git CVEs land at unpredictable intervals. Auto-security-updates is the difference between "patched within 12 hours" and "vulnerable until I notice the news".

### Don't write the gateway PAT into `gateway.toml` (URL-embedded credential)

The old pattern was `upstream = "https://<token>@gitea/repo.git"`. That puts the credential in cleartext in a config file alongside other config: easy to leak via `cat`, `grep`, backup tooling, etc. The new model puts the URL in `gateway.toml` and the credential separately in `<policyRoot>/<repo>/credential` (mode 0600). Don't revert to embedded URLs.

### Don't share an upstream credential across repos that don't need it

If one repo's PAT leaks and it has scope across many gitea repos, every repo is exposed. Per-repo PATs scope blast radius. You can mix: shared PAT for low-stakes repos, per-repo PAT for critical ones.

### Don't whitelist with `frame = "*"`

The whitelist syntax allows `frame = "*"` (suppress every frame for matching paths). Useful for legitimately-binary directories (vendored deps, test fixtures). Easy to over-apply. Prefer per-frame entries even when verbose.

---

## Monitoring: knowing if the model fails

### Audit log

Every push is logged at `/srv/gateway/cfg/<repo>/audit.log` with: timestamp, ref names, SSH key fingerprint, decision (accept / observe / reject), frames fired, frames suppressed by whitelist. Tail it or aggregate it elsewhere: that's your forensic record.

### Dashboard `/events`

Same data, browser-visible. Filterable by repo / time / event type. Use this when investigating "what happened around 03:14 last Tuesday?"

### Dashboard `/feed`

Live push stream. Sit on this during high-trust periods (a new agent's first day pushing, after rotating credentials, etc.).

### Things to watch for

- **Pushes from SSH key fingerprints you don't recognize.** Cross-reference with the `/ssh-keys` page; if a key is in `authorized_keys` you don't remember adding, revoke it.
- **Repeated relay failures to the same upstream.** Usually a stale credential, occasionally an upstream-side block (e.g. gitea revoked the PAT).
- **A whitelist entry that fires on every push.** Either the underlying frame is over-broad and needs tightening, or the entry's path glob is too wide. Tightening the entry beats disabling the frame.
- **An audit-log gap.** Audit log is append-only on disk; gaps mean the dashboard service crashed or someone wrote to the file directly. Investigate.

---

## Incident response: first 15 minutes

If you suspect a push made it through that shouldn't have:

1. **Identify the push.** Dashboard `/feed` or `/events`, narrow by repo + time window.
2. **Revoke the credential it used.**
   - SSH key: dashboard `/ssh-keys` → delete the fingerprint
   - Upstream PAT: gitea → settings → applications → revoke
3. **Roll the upstream back if needed.** From any clone, `git push --force-with-lease origin <prev-good-sha>:main`. Note: this can also be pushed through the gateway (with policy enforcement) or directly to gitea if the gateway itself is suspected.
4. **Read the audit log entry** to confirm what was suppressed by whitelist vs. what fired and was overridden by observe mode vs. what genuinely passed. The frames that should have caught it are listed.
5. **Tighten the policy**: disable the whitelist entry that helped, change the frame's severity, add a new frame, etc.

---

## Quick verification checklist (run quarterly)

```bash
# 1. git version current?
ssh root@<gateway> 'git --version'

# 2. unattended-upgrades active?
ssh root@<gateway> 'systemctl is-enabled unattended-upgrades'

# 3. git user shell still git-shell?
ssh root@<gateway> 'getent passwd git | cut -d: -f7'

# 4. ~git/.git-shell-commands/ still absent?
ssh root@<gateway> 'ls ~git/.git-shell-commands/ 2>&1 | head -1'

# 5. git-shell rejects bash from an authorized key
ssh git@<gateway> 'echo testing'        # expect: fatal: Interactive git shell is not enabled.

# 6. dashboard service still runs as git, not root
ssh root@<gateway> 'systemctl cat nimblegate-dashboard.service | grep ^User='

# 7. credentials still in cred files, not URLs
ssh root@<gateway> 'for r in /srv/gateway/cfg/*/; do
  grep -E "^upstream" "$r/gateway.toml" 2>/dev/null | grep -E "//[^@/]+@" && echo "WARNING: $r has embedded credential"
done'

# 8. Audit log not corrupted (append-only, monotonic timestamps)
ssh root@<gateway> 'for r in /srv/gateway/cfg/*/audit.log; do
  echo "=== $r ==="
  tail -3 "$r" 2>/dev/null
done'
```

**Dev-machine checks**, run on every box where an agent runs (the gateway is only a boundary if these all pass):

```bash
# 9. No HTTPS credential cache for any upstream host
test ! -s ~/.git-credentials && echo "OK: no creds file" || cat ~/.git-credentials   # any output = bypass

# 10. Dev machine's SSH key refused by every upstream you use
for host in github.com gitlab.com bitbucket.org <your-gitea-host>; do
  ssh -o BatchMode=yes -o ConnectTimeout=4 -T git@$host 2>&1 \
    | grep -qE "(successfully authenticated|Welcome|logged in)" \
    && echo "BYPASS at $host" || echo "OK at $host"
done

# 11. Every local repo's origin points at the gateway, not upstream
find ~ /media -maxdepth 5 -name "config" -path "*/.git/*" 2>/dev/null \
  | xargs grep -lE "url = .*(github\.com|gitlab\.com|<your-gitea-host>)" 2>/dev/null
# any output = a project that bypasses the gate; re-point its origin
```

Anything that doesn't match expectations is worth investigating before it becomes a real incident. For dev-machine setup + retrofit + the credential-store sweep (macOS keychain, libsecret, gh / glab / tea CLIs, gpg-encrypted stores), see [`DEV-MACHINE-SETUP.md`](DEV-MACHINE-SETUP.md).

---

## What this document is not

- **Not the dev-machine hygiene guide.** This doc covers the GATEWAY side. The companion piece (how to keep the box your agent runs on from holding direct upstream credentials) is [`DEV-MACHINE-SETUP.md`](DEV-MACHINE-SETUP.md). The gate's value depends on both being followed.
- **Not a complete threat model for your business.** It addresses the gateway specifically. Your laptop, CI runners, gitea host, and any internal services that consume gitea are separate problems.
- **Not a substitute for keeping the OS patched.** Distro security updates handle 90% of practical risk.
- **Not legal / compliance advice.** If you're under SOC2 / ISO27001 / HIPAA, talk to whoever audits you about whether this configuration meets your obligations.

For deeper questions about specific frames, severity tuning, or kit composition, see [`docs/frames.md`](../frames.md). For deployment specifics (the bare-binary install, the container variant, the migration playbook), see the other docs in this directory.
