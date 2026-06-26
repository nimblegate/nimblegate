# Repos

The repos page is where you register a repo for the gateway to guard and manage its lifecycle. Each registered repo gets its own bare git repo on disk + per-repo policy + an optional upstream credential for forwarding pushes to the real remote.

## Per-row actions

- **Edit policy**: links to [/policy?repo=&lt;name&gt;](/policy) where kits, frame toggles, severity overrides, and whitelist live.
- **Edit repo settings**: a collapsible under each repo row to change the **upstream URL** and **protected refs** in place (no delete + re-add). Use `refs/heads/*` to gate every branch (needed for the auto-PR flow). This gates push **content**; branch **deletion** is controlled separately, so `main` stays protected while feature branches gated via `refs/heads/*` remain deletable. Name and credential aren't edited here (rename = delete + re-add; the credential has its own rotate form).
- **Archive**: soft-removes from the active list; files preserved in `_repos/`. Pushes fail until restored. Archived repos appear in a collapsible panel at the bottom; **Restore** moves them back.
- **Delete permanently** (archived rows only): the irreversible counterpart to Archive - removes the bare repo (all git history) and the policy/credential dir, freeing the name **and** the upstream URL for re-registration. The upstream remote and other repos are untouched. Use this to fix a mis-registered repo (wrong name, wrong upstream): Archive it, then Delete permanently, then add it again cleanly. Confirm-gated; there is no undo.

## Add new repo

The "+ Add new repo to gateway" form takes:

- **Name**: letters, numbers, hyphens, underscores. Must be **unique** - registering a name that already exists is rejected with a visible error (no silent no-op). This becomes the push path `<name>.git`.
- **Upstream URL**: use **HTTPS** (`https://github.com/you/app.git`) - the container relays over HTTPS only, and a PAT authenticates it, including **private** repos. The container ships **without an SSH client by design** (minimal image, smaller attack surface), so an `ssh://` / `git@…` upstream is **rejected at registration** - use HTTPS. (Relaying an SSH upstream via a deploy key is possible only if you add an ssh client to the container image yourself - an opt-in operator task, not the default.) Registering the same upstream under a second name is also blocked (almost always a mistake).
- **Upstream credential**: for an HTTPS upstream, a **Personal Access Token** scoped to **write that repo** - GitHub fine-grained: *Contents → Read and write*; GitHub classic: `repo`; Gitea: `write:repository`; GitLab: `write_repository`. Stored mode 0600, never logged. *(A PAT is ignored for an `ssh://` upstream - that path uses a deploy key, not a token.)*
- **Protected refs**: space-separated; default `refs/heads/main`. Use `refs/heads/*` to check every branch.
- **Status checkboxes**: `enabled` (default on) and `observe-only` (default off). These set the *initial* state for the new repo; later changes flow through pushes / policy, not /repos.

After registering, if the upstream **already has commits**, click **Sync from upstream** on the repo row so the gateway mirrors that history (so existing clones push cleanly); a brand-new/empty upstream needs no sync. A red **relay failing** badge on a row means the gateway *accepted* pushes but the most recent relay to the upstream **failed** - pushes aren't reaching your real host; check the upstream URL (use `https://` for a PAT) and the credential.

The bare repo also gets `git config receive.maxInputSize 500m` applied at registration: a 500 MiB cap on incoming pack files that closes the disk-fill DoS vector. Override per repo by editing `<policy-root>/<repo>/gateway.toml` `max-input-size = "1g"` (or `"0"` for unlimited) and re-applying with `git -C /srv/gateway/repos/<repo>.git config receive.maxInputSize <value>`. Repos with binary-heavy content should use Git LFS at the upstream rather than raising this; see SECURITY-MODEL.md "Git LFS interaction" for the gating trade-off.

If you run `gateway add` from a CLI session under a different Unix user than the one that owns `<repos-root>` (typical case: running `nimblegate gateway add` via `ssh nbg-admin` as root when the bare repos are git-owned), the new files get chowned to match the existing repos root's owner automatically. Without this, git-shell would reject the next push as "dubious ownership" because the bare repo would be owned by root while git-receive-pack runs as the git user. The auto-chown is a no-op when you're already running as the right user.

## Auto-generated files

Registration writes every file a fully-wired repo needs, so there's no "click Apply kit → nothing happens" trap on a fresh registration. As of v0.1.0 the seed includes:

- `<policy-root>/<repo>/gateway.toml`: per-repo upstream URL + protected refs + status (mode 0600 because notification webhooks may carry secrets).
- `<policy-root>/<repo>/appframes.toml`: empty `[frames]` section so the dashboard's frame toggle / kit apply handlers find a parseable file. Without this seed, the first click silently no-ops until a save creates it; the trap surfaced during ai-assistant onboarding.
- `<repos-root>/<repo>.git/`: bare repo with `receive.maxInputSize` cap applied and pre/post-receive hooks installed.
- Activation symlinks at `<policy-root>/<repo>` and `<repos-root>/<repo>.git` pointing at the `_repos/` lib paths (so archive removes the symlink only, preserving files).

The credential file is NOT auto-generated (we don't have a token to seed). For HTTP upstreams use the "Add or rotate upstream credential" section below; for SSH upstreams no per-repo file is needed.

## Issues to address

If any registered repo is missing a file the gateway expected, the page shows an "Issues to address" banner under the active-repos table listing each finding with:

- **Repo**: which registration the issue belongs to
- **File**: the missing or malformed file (e.g. `appframes.toml`, `demo.git`, `credential`)
- **Severity**: `blocking` (the repo can't function, pushes will fail) or `degraded` (works, but a dashboard feature will misbehave)
- **What / Why**: one sentence each on the symptom and the consequence
- **Action**: a `Repair` button for issues the dashboard can fix (regenerate `appframes.toml` with a default empty `[frames]` section), or `operator action` text when only the operator can resolve (missing bare repo needs restore from `_repos/`; missing credential for an HTTP upstream needs a PAT pasted via the rotate form).

The banner stays hidden when every repo is connected, no clutter in the common case. Clicking Repair regenerates the missing file with a safe default and logs a `repo-connection-repair` event to the audit log.

## Credential badge meanings

Each row's status column shows one of three credential badges depending on the upstream URL shape + whether a credential file exists on disk:

- **credential set** (filled pill): a PAT or deploy token sits at `<policy-root>/<repo>/credential` mode 0600. Normal state for `https://` relays.
- **credential n/a (SSH)** (dashed pill): upstream URL is SSH-shaped (`ssh://...` or `git@host:path`), relaying via the gateway's own SSH identity, so no per-repo credential is needed. You only see this on a gateway where an ssh client was **added to the container yourself** (the opt-in SSH path) - the default container rejects SSH upstreams at registration, so the normal state is an HTTPS upstream with **credential set**.
- **credential unset** (plain pill): upstream URL is HTTP but no credential file exists. Relays will fail with a 401 / Permission denied. Either install a PAT via the "Add or rotate upstream credential" section below, or convert the upstream URL to SSH if the gateway is configured for SSH relay.

## Add or rotate upstream credential

A separate "Add or rotate upstream credential" section sits below the repos table. Pick a repo from the dropdown, paste a new token, submit. Overwrites the existing one. Previous value is gone after submit; no audit trail beyond a "credential-update" event.

## Connecting an existing upstream

Registration **mirrors the upstream's existing history into the gateway bare repo automatically** (default-on), so a repo whose upstream already has commits is clone-able from the gateway the moment you add it, with no manual server-side seeding.

- **New repo (empty upstream)**: nothing to mirror. The developer's first push flows through the gate and creates the upstream content.
- **Existing repo (upstream already has commits)**: at registration the gateway fetches the upstream's branches + tags and points HEAD at the upstream's default branch, so `git clone <gateway>/<name>.git` checks out the files directly.

If the registration-time mirror can't complete (an HTTP upstream whose credential isn't set yet, or an upstream that's briefly unreachable) the repo still registers and an **Issues to address** entry appears with a one-click **Sync** button. Set the upstream credential (HTTP upstreams) via the rotate form, then click Sync to pull the history. SSH upstreams authenticate with the gateway's own key, so their mirror runs without a per-repo credential.

The CLI `gateway add` imports by default too; pass `--no-import` to register without pulling history.

## Common gotchas

- The credential is for the gateway's forward leg (pushing to the real upstream), not for your agent. Your agent authenticates with SSH at port 2222.
- The push path is **flat**: `<name>.git`, no `owner/` namespace. An upstream like `git@host:owner/repo.git` is just `<name>.git` on the gateway; the `owner/` prefix exists only on the upstream side.
- First push triggers an auto-scan that recommends additional kits on [Policy](/policy).

For depth: [README: Quick install](https://github.com/nimblegate/nimblegate/blob/main/README.md#quick-install).
