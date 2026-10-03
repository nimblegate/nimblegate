# Quick guide: let your agent ask the gate (MCP)

The gateway has a read-only MCP endpoint, so an AI agent can ask about its own
pushes: which rules fired this week, which findings keep coming back, what was
rejected and why. Optional: pushing through the gate needs none of this. The
full reference is [Agent stats API](agent-api.md).

## 1. Create a token

On the gateway machine:

```bash
docker exec -u git nimblegate nimblegate gateway token new my-agent
```

It prints a token starting with `nbg_` once; copy it. Bare-metal install: run
`nimblegate gateway token new my-agent` directly. Use one token per agent, so
you can revoke one without the others.

## 2. Make port 7900 reachable for the agent

The endpoint is on the dashboard port, which listens only on the gateway's own
loopback. If the agent runs on another machine, open the same tunnel you use
for the dashboard and leave it running:

```bash
ssh -L 7900:127.0.0.1:7900 <user>@<gateway-host>
```

## 3. Add the MCP server to your agent

Claude Code:

```bash
claude mcp add --transport http nimblegate http://127.0.0.1:7900/mcp \
  --header "Authorization: Bearer nbg_..."
```

Other MCP clients: add an HTTP server with URL `http://127.0.0.1:7900/mcp` and
the header `Authorization: Bearer nbg_...`.

## 4. Ask it something

For example: "Which nimblegate rules fired most on my-app in the last 7 days?"
or "Show the pushes the gate rejected this week and why."

The agent can only read decision history. It cannot change policy, approve
anything or bypass the gate.

## Next

- [Agent stats API](agent-api.md) - all seven tools, the REST routes and token management.
- The dashboard's **Reports** page answers the same questions without a token.
