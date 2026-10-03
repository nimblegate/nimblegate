# Quick guide: access tokens for GitHub, Gitea and GitLab

The gateway forwards clean pushes to your real repo with a personal access
token. It needs permission to push, and a little more if you use Auto-PR to
post findings as pull request comments. Give it nothing wider.

## 1. Pick the scopes

| Host | Push only | Push and Auto-PR comments |
|---|---|---|
| GitHub, fine-grained token | Contents: Read and write | add Pull requests: Read, Issues: Read and write |
| GitHub, classic token | `repo` | `repo` (covers both) |
| Gitea | `write:repository` | add `read:repository`, `write:issue` |
| GitLab | `write_repository` | Auto-PR comments are GitHub and Gitea only for now |

A GitHub fine-grained token is limited to the repos you pick, so it's the
tightest choice. PR comments use GitHub's Issues API, hence the Issues
permission.

## 2. Create the token on your git host

- **GitHub fine-grained:** Settings → Developer settings → Personal access
  tokens → Fine-grained tokens → **Generate new token**
  (`github.com/settings/personal-access-tokens/new`). Under **Repository
  access** pick the repo, then set the permissions above. For an organization's
  repo, set **Resource owner** to the organization; it may need an admin's
  approval.
- **GitHub classic:** Settings → Developer settings → Personal access tokens
  → Tokens (classic) → **Generate new token** (`github.com/settings/tokens/new`).
- **Gitea:** avatar → Settings → Applications → **Generate New Token**
  (`<your-gitea>/user/settings/applications`).
- **GitLab:** avatar → Edit profile → Access tokens → **Add new token**
  (`gitlab.com/-/user_settings/personal_access_tokens`).

Copy the token when it's shown; most hosts show it only once. Note the expiry
date: when it passes, pushes stop reaching your real repo.

## 3. Give it to the gateway

- **New repo:** paste it into **Upstream credential** in **Repos → Add a
  repo**, with the repo's **HTTPS** URL as the upstream.
- **Existing repo, or a new token after expiry:** on the **Repos** page, use
  **Add or rotate upstream credential** for that repo.

The token is stored on the gateway only, readable by the gateway and never
logged. Your dev machine and your agents never see it.

## 4. Check it works

Push something small through the gateway and look at the repo on your git
host: the commit should be there within seconds. If the push was accepted but
never arrives, the **Repos** page shows a red **relay failing** badge, which
almost always means a wrong scope or an expired token. Fix the token, rotate
it, and use **Sync from upstream** if the repo's history is missing.

Using Auto-PR? If comments don't appear, open **Auto-PR → Repos**: an HTTP 403
there means the token lacks the comment permission from step 1.

## Next

- [Notifications and Auto-PR](notifications.md) - the fix loop that uses the comment permission.
- [Full setup guide](getting-started.md#step-4-register-the-repo-to-guard) - SSH deploy keys and other upstream options.
