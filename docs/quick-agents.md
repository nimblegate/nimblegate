# Quick guide: run your AI agent through the gateway

Claude Code, Cursor, Copilot, Aider or your own scripts need no setup. Once a
repo's remote points at the gateway, every `git push` from it, yours or the
agent's, is checked. Three things make that hold. The details are in
[Connecting machines and agents](connecting.md).

## 1. Point the working copy at the gateway

Do this in every clone or worktree the agent uses:

```bash
git remote set-url origin ssh://git@<gateway-host>:2222/~/<repo-name>.git
git remote -v        # both lines should show the gateway
```

If the agent clones fresh, have it clone from the same gateway URL. A clone
taken straight from GitHub pushes straight to GitHub.

## 2. Close the side door

The gate only holds if the agent can't push to GitHub directly. On the machine
where the agent runs:

```bash
rm -f ~/.git-credentials
git config --global --unset credential.helper
ssh -o BatchMode=yes -T git@github.com 2>&1 | grep -q "Permission denied" \
  && echo "OK: github refused" || echo "BYPASS: this key works on github directly"
gh auth status       # a logged-in gh CLI is another way around the gate
```

If you see `BYPASS`, remove that key from your GitHub account or use a
separate key for the gateway.

## 3. Let the agent read the rejection

When a push trips a rule, the reason is in the `git push` output: the rule, the
file and the line. An agent that reads its command output fixes the file and
pushes again, and the clean push goes through. One line in the agent's
instructions helps:

```
If git push is rejected by the gateway, read the findings, fix them, and push again. Never bypass the gateway.
```

Agent in a container or sandbox? It needs an SSH key registered on the
gateway too: mount yours, or create one inside and add it under **Keys**.

## Next

- [Choose what gets checked](quick-frames.md) - add the agent-shortcuts kit to catch skipped tests and weakened CI.
- [Multiple agents](multi-agent.md) - several agents on one repo, overlap alerts and hand-offs.
- [Notifications and Auto-PR](notifications.md) - findings as PR comments and webhooks.
