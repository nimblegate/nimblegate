# Quick guide: choose what gets checked

Every registered repo starts with the **core** kit: leaked credentials and
private keys, force-pushes to protected branches, `--no-verify`, `rm -rf` of
protected paths, `curl | sh` and a few more. Add more in a few clicks. The
details are in [Choosing what the gate checks](policy-authoring.md).

## 1. Open the repo's policy

In the dashboard: **Repos**, then **Edit policy** on the repo's row. Scroll to
**Frame selection** at the bottom.

## 2. Add a kit

Click a kit in the **Quick start** row. Kits stack:

| Kit | Add it when |
|---|---|
| `agent-shortcuts` | AI agents push to the repo: skipped tests, tests that can't fail, CI told to ignore failures. Warns only. |
| `web-app` | The repo ships HTML pages: meta tags, alt text, mixed content. |
| `cf-pages-project` | SvelteKit, Astro or Next on Cloudflare Pages. |
| `cf-workers-project` | Cloudflare Workers, no HTML. |
| `security-strict` | Every security frame, including invisible-Unicode tricks. |
| `encoding-strict` | Paste corruption: curly quotes in config, BOMs, tabs in YAML. |

Every frame is listed with what it catches in the
[frame reference](https://nimblegate.com/docs/frames/).

## 3. Try it before it blocks

Not sure what a new kit will flag in an existing repo? Click **Switch to
observe** on the repo's row for a while: every push is checked and recorded on
the **Feed**, and nothing is rejected. **Switch to enforce** when you're happy.

BLOCK findings reject the push. WARN and INFO findings let it through and are
recorded.

## 4. Allow a real exception

If a finding is intended, such as a test fixture that looks like a key, add a
whitelist entry with a written reason instead of turning the frame off. It is
recorded in the audit log on every push. See
[Groups and whitelist](groups-and-whitelist.md).

## Next

- [Frame reference](https://nimblegate.com/docs/frames/) - every check, its severity and how to fix a finding.
- [Writing your own frames](frame-authoring.md) - your own rules, as regex or code.
