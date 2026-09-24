# Settings

The tab strip splits Settings into four views. **System** (default) is read-only install info; **Display** is browser-only preferences; **Commit links** decides where short commit SHAs link to; **About** is the license + project links. None of the tabs here change gate behavior or policy.

## System

Read-only snapshot of this install:

- **Install type**: `container (nimblegate image)` if s6-overlay is present, `container (Docker)` for a generic container, or `bare metal` for a host install. Detected by probing `/etc/s6-overlay`, `/.dockerenv`, `/proc/1/cgroup`, and the systemd unit.
- **Build**: short commit SHA + build date stamped at compile time via `runtime/debug.ReadBuildInfo()`. Use this to confirm the running binary matches what you deployed.
- **Policy root / repos root / SSH keys file**: the paths the dashboard reads/writes. Useful when triaging permission errors.
- **Auth mode**: `setup-token` (single-admin bcrypt sessions) or `off` (reverse-proxy fronts auth).
- **Started / uptime**: process start time + how long it's been running.

## Commit links

Short commit SHAs on the [Feed](/feed) and [Overlaps](/overlaps) pages link to the commit on your git host when the dashboard can work out its web address:

- An `https://` upstream links on its own address (GitHub, Gitea, Forgejo, GitLab, Bitbucket).
- An SSH upstream links on its own only for github.com, gitlab.com, bitbucket.org and codeberg.org.
- Anything else needs a **host mapping**, one line per git host: `<host> <web-url> [gitlab|bitbucket]`, for example `192.168.1.20 http://192.168.1.20:3000` for a LAN Gitea whose web UI runs on port 3000. Add `gitlab` or `bitbucket` when the host runs one of those, since their commit URLs differ.

The **What each repo links to** table shows the link every repo produces, so you can check a mapping before relying on it. A repo with no link keeps click-to-copy: clicking its SHA copies the full commit ID. The dashboard never guesses a web address, because a wrong link is worse than none. Mappings are stored in `<policy-root>/weblinks.toml`, separate from the hand-edited `gateway.toml`.

## Display

Browser-only preferences. All values live in `localStorage`; nothing here syncs across devices or affects the server.

- **Sidebar starts**: left nav rail opens **expanded** or **collapsed** by default.
- **Feed auto-refresh**: how often [Feed](/feed) polls for new pushes: **Off**, **every 5s**, **every 15s**, or **every 30s**.
- **Timestamp timezone**: show times in your **Local** timezone or in **Server (UTC)**. Affects feed + events + stats.
- **Timestamp color**: per-day tint on timestamps **On** or **Off**.
- **Day grouping**: group feed / events rows by day with date headers, **On** or **Off**.

## About

License + project pointers:

- **License**: nimblegate is licensed under **PolyForm Noncommercial 1.0.0**, a source-available license with free and unrestricted non-commercial use, today and for good. Commercial use requires a commercial license (email contact@nimblegate.com).
- **Project**: GitHub source + releases, the nimblegate.com website, GitHub sponsors, security disclosures.
- **Updates**: nimblegate never phones home or checks for updates. Watch the GitHub releases page to know when a new version is out.

## Common gotchas

- A different browser (or incognito window) starts with the Display defaults again; nothing there syncs.
- Clearing site data resets every Display preference.
- Auth + password are managed via [Login](/login) / [Setup](/setup), not on the Settings page.
- The Build SHA shown here is the running binary's, not the latest on disk. Restart the service after a swap.

For depth: [README: Operator visibility](https://github.com/nimblegate/nimblegate/blob/main/README.md#operator-visibility).
