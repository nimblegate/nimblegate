# Notification rail: operator guide

The gateway can notify the upstream (PR comment) and a webhook URL when a push is rejected. Both channels carry the same JSON payload: agents listening for either rail get the same structured data. This guide covers how the rail works, how to wire up a webhook receiver, and how to read the configuration knobs.

For the design rationale + invariants, see [`docs/superpowers/specs/2026-06-04-auto-pr-and-webhook-design.md`](superpowers/specs/2026-06-04-auto-pr-and-webhook-design.md). For the adapter-author view, see [`docs/adapters.md`](adapters.md).

---

## What happens on a rejected push

1. **Pre-receive** runs the policy frames, decides reject, writes to the per-repo audit log (existing behavior).
2. **Queue write**: a record is appended to `<policy-root>/<repo>/pr-comment-queue.jsonl` *before* any network call. This is the durability anchor: subsequent failures only affect WHEN delivery lands, not WHETHER.
3. **Opportunistic inline attempt**: with a 3s timeout, pre-receive tries to deliver the notification right away. If it succeeds, the queue record is removed and the push completes silently (no extra line in the push response). If it times out or fails, the queue record stays for the daemon to drain on its next sweep.
4. **Daemon drain**: the dashboard process polls every 5s, picks up queue records older than 30s, calls the upstream adapter + fires the webhook. Failed records get an exponential backoff (1m → 5m → 30m → 2h) until either success or the configured `delivery.max-attempts` cap, after which they move to `pr-comment-deadletter.jsonl` for operator investigation.

If there's **no open PR on the rejected ref**, the PR-comment step is skipped silently; the webhook still fires. Direct-to-main pushes still produce a notification to whatever receiver is wired up.

**Prerequisite - gate the branches agents push to.** The loop fires on a *rejected* push, and only **gated** refs are checked. Coding agents work on feature branches (often in git worktrees) and open one PR per branch, so the rejected ref is a feature branch, not `main`. New repos **default their protected refs to `refs/heads/*`** (Repos → the repo → Edit policy → **Edit repo settings**), so feature-branch pushes are gated out of the box. If you narrow it to `refs/heads/main`, only `main` is gated and feature-branch pushes sail through ungated and never trigger the loop.

## What the consumer sees

### PR comment

A sticky comment is posted on the PR for the rejected ref. Same comment edits in place across subsequent rejects (no new comment per attempt). The comment includes:

- Status line (⛔ rejected / 🔄 rotated / ⛔⛔ exhausted / ⚠ observe-mode)
- Mention line tagging the configured bot + PR's auto-tagged human assignees
- Findings table grouped by severity (BLOCK / WARN / INFO)
- `<details>` history block listing previous attempts on the same PR
- Hidden HTML data block: `<!-- nimblegate-data: {...full JSON payload...} -->` (agents fetching the raw comment body extract this for structured processing)

### Webhook

POST to the configured URL with `Content-Type: application/json` and the same JSON payload as the comment's hidden block.

Auth modes:

- **`hmac`** (recommended): `X-Nimblegate-Signature: sha256=<hex>` header. The signature is HMAC-SHA256 of the raw request body using the configured secret.
- **`bearer`**: `Authorization: Bearer <token>` header (useful when the receiver already authenticates with bearer tokens from other sources).
- **`none`**: no auth header. LAN-only deployments where adding auth is friction without benefit.

## Configuration

Per-repo `<policy-root>/<repo>/gateway.toml` gains a `[notification]` block. Defaults shown:

```toml
[notification]
enabled = false                           # set true to fire notifications for this repo
observe-pr-comments = false               # opt-in: also notify in observe mode

[notification.webhook]
url = ""                                  # empty = webhook disabled, PR-comment-only
auth-mode = "hmac"                        # "hmac" | "bearer" | "none"
secret = ""                               # HMAC signing key OR Bearer token value
auth-header = ""                          # optional override

[notification.mention]
default = "@nimblegate-bot"               # single-bot default (used when rotation disabled)
include-pr-assignees = true               # auto-tag PR's human assignees/reviewers

[notification.mention.rotation]           # opt-in - empty bots = no rotation
bots = []                                 # e.g. ["@claude-code-bot", "@cursor-bot"]
attempts-per-bot = 2
rotate-on-repeat-finding = true
fallback-human = ""

[notification.loop]
max-attempts = 5                          # rejected pushes per PR before loop exhaustion
cooldown-threshold-count = 3
cooldown-threshold-window = "5m"
cooldown-duration = "10m"

[notification.delivery]
max-attempts = 20                         # daemon retries per record before deadletter
backoff-schedule = ["1m", "5m", "30m", "2h"]
```

Or set via the dashboard at `/policy?repo=<name>` → expand "Notification rail" → check "Enable notifications for this repo" → Save.

**Defaults are conservative.** With only `enabled = true` and nothing else, you get:

- PR comments with `@nimblegate-bot` + PR assignees auto-tagged
- Loop guardrails active (max 5 attempts, cooldown if flooding)
- No webhook
- No multi-bot rotation

Multi-bot rotation, webhook integration, and observe-mode PR comments are all opt-ins.

## Webhook receivers: examples

### Go

```go
package main

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "io"
    "net/http"
)

const secret = "your-shared-secret"

func main() {
    http.HandleFunc("/nimblegate", func(w http.ResponseWriter, r *http.Request) {
        body, _ := io.ReadAll(r.Body)
        sig := r.Header.Get("X-Nimblegate-Signature")
        if !verifyHMAC(secret, body, sig) {
            w.WriteHeader(401)
            return
        }
        // body is the JSON payload - decode and act on it
        w.WriteHeader(200)
    })
    _ = http.ListenAndServe(":8080", nil)
}

func verifyHMAC(secret string, payload []byte, signature string) bool {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(payload)
    expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
    return hmac.Equal([]byte(expected), []byte(signature))
}
```

### Python

```python
import hmac, hashlib
from flask import Flask, request

app = Flask(__name__)
SECRET = b"your-shared-secret"

@app.post("/nimblegate")
def webhook():
    body = request.get_data()
    sig = request.headers.get("X-Nimblegate-Signature", "")
    expected = "sha256=" + hmac.new(SECRET, body, hashlib.sha256).hexdigest()
    if not hmac.compare_digest(expected, sig):
        return "", 401
    payload = request.get_json()
    # act on payload
    return "", 200
```

### Node.js

```js
import express from "express";
import crypto from "crypto";

const app = express();
const SECRET = "your-shared-secret";

app.post("/nimblegate", express.raw({ type: "application/json" }), (req, res) => {
  const sig = req.header("x-nimblegate-signature") || "";
  const mac = crypto.createHmac("sha256", SECRET).update(req.body).digest("hex");
  if (!crypto.timingSafeEqual(Buffer.from(`sha256=${mac}`), Buffer.from(sig))) {
    return res.status(401).end();
  }
  const payload = JSON.parse(req.body.toString());
  // act on payload
  res.status(200).end();
});

app.listen(8080);
```

## Loop guardrails

Three mechanisms prevent runaway loops on a stuck PR:

- **Attempt counter**: every reject on the same PR increments `loop.attempt_count`. When it reaches `max-attempts` (default 5), the loop is marked `exhausted`. Bot mentions stop; `fallback-human` (if configured) is tagged instead.
- **Same-finding-twice fast rotation** (when multi-bot rotation is enabled): if two consecutive rejects produce the same `(frame_id, file, line)` fingerprint, the current bot rotates immediately rather than waiting for the per-bot attempt threshold. Signal: "this agent is not making progress on this finding."
- **Cooldown**: if 3+ rejects arrive within 5 minutes on the same PR, the daemon pauses delivery for 10 minutes (queue records still write for audit). Stops a runaway agent from flooding a PR with 50 "still broken" comments in a minute.

Loop state lives at `<policy-root>/<repo>/pr-comment-state/<pr>.json`. Inspect with `cat`; reset via the dashboard's "Reset loop" button or `rm` the file.

## Operator visibility

`/health` page shows queue depth per repo, last drain timestamp, deadletter count, daemon status, and recent delivery success rates.

`/feed` rows show a per-row notification pill (`delivered` / `queued` / `deadlettered`) recovered at read time by correlating each row's notification EventID against the queue/deadletter files (the append-only audit log isn't mutated). Active loops surface "attempt N/M with @bot" and a Reset Loop button.

**The loop closes on the fix, not on merge.** A clean push to the gated ref is the convergence signal: the gateway flips the sticky PR comment to "✅ All findings resolved", fires a resolution webhook, and clears the dashboard loop. (Reset Loop is the manual escape hatch if a loop wedges.)

`/auto-pr` → **Activity** is the rail timeline across repos: rejection deliveries **and** `✅ resolved` events, each with its live outcome. each with its live outcome.

`/policy?repo=<name>` page has the full notification rail config form when expanded.

## Troubleshooting

**Comment didn't post.** The **Auto-PR → Repos** tab now shows the failure inline - a warning row under the repo with the upstream error + an actionable hint (no need to read `docker logs`). The daemon log still has the detail: `docker compose logs nimblegate | grep "notification daemon"` → e.g. `deliver evt_… (repo X): find PR: upstream: permanent error: HTTP 403`. Common causes:

- **HTTP 403 (the most common first-time failure): the upstream token lacks the PR-comment permission.** Relay (`git push`) and commenting use *different* permissions, so relay can succeed while comments 403. The required scopes, verified against each host's docs:

  | Action | **GitHub** (fine-grained) | **Gitea** | **GitLab** |
  |---|---|---|---|
  | relay (`git push`) | Contents: Read and write | `write:repository` | `write_repository` |
  | find the PR/MR | Pull requests: **Read** | `read:repository` | `read_api` |
  | post/edit the comment | **Issues: Read and write** (PR comments use the Issues API) | `write:issue` | `api` |

  Simplest per host: **GitHub** - a classic token with the **`repo`** scope (covers all three). **Gitea** - `write:repository` + `read:repository` + `write:issue`. **GitLab** - the **`api`** scope (full read/write; there's no narrower scope that permits MR comments, and `api` covers the push + find too). Regenerate with the right scopes and rotate the token on `/repos`, then click **Retry now** (below).
- Wrong webhook URL, revoked/expired upstream PAT, or the PR was closed/merged between push and delivery.
- Upstream URL registered as SSH (`git@…`) while a PAT is set: the comment API needs an **HTTPS** upstream URL so the token is used.

**Same comment posted multiple times.** The sticky-comment ID may have been lost (operator deleted the comment manually). The orchestrator falls back to `ScanForMarker` which scans existing PR comments for the hidden HTML block: if it finds one, it updates that comment instead of creating a new one. If scan also fails, a new comment is created.

**Records stuck / deadlettering after a fix.** When you fix a bad token, the *already-failed* records don't deliver instantly - each failure grows their retry backoff (toward a ~2h cap), and enough failures move them to the deadletter. **Click "Retry now"** on the repo's row in **Auto-PR → Repos** (visible whenever the queue or deadletter is non-zero): it resets the backoff and re-queues any deadlettered records, so everything retries on the next ~5s poll - no server access needed. The equivalent manual recovery on the box is:

```bash
# move deadletter records back to queue for retry (Retry now does this for you)
cat pr-comment-deadletter.jsonl >> pr-comment-queue.jsonl
truncate -s 0 pr-comment-deadletter.jsonl
```

**Queue grows but daemon never drains.** The daemon runs alongside the dashboard HTTP server. For the container install: `docker logs nimblegate` to inspect, `docker compose restart` to bounce it. For a bare-metal install: `systemctl status nimblegate-dashboard` and `systemctl restart nimblegate-dashboard` (the systemd unit kept the `nimblegate` codename; only the public brand was renamed in v0.1.0).

**Webhook returning 401.** Receiver's HMAC verification is failing. Check that secret matches what's configured in the gateway (`grep secret <policy-root>/<repo>/gateway.toml`) and that the receiver computes HMAC over the **raw request body** (not the parsed JSON, since parsing reorders fields and changes whitespace).
