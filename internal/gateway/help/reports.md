# Reports

One-click reports built from the gate's own decision log: no agent, no API token, no query language. Pick a repo and a window, click a report, read the result. These are the same numbers an AI agent gets from the [agent stats API](https://github.com/nimblegate/nimblegate/blob/main/docs/agent-api.md), here behind your dashboard login.

## Controls

- **Repo**: one repo, or **all repos** (pooled into one result).
- **Window**: `Last 7 days` / `Last 30 days` / `Last 90 days` / `Last year`. Day-windowed; there is no all-time view (default 30 days).
- **Rows**: `25 / 50 / 100 / 250 / 500` (default 50). The cap on list reports; for the aggregate reports it's the top-N. A wide window with a low Rows shows only the newest N. Raise it to see the rest.

## The reports

- **What changed**: recent commits in the repo(s), each push-tip tagged with the gate's verdict.
- **Gate stats**: decisions / accepted / rejected for the window, plus the top firing rules.
- **Bounce rate**: per repo, the share of pushes the gate rejected.
- **Top rules**: which frames fire most (filter by severity).
- **Time saved**: modeled debugging hours the gate prevented, by frame.
- **Recurring findings**: what keeps coming back across pushes (fix it at the source, or whitelist with a reason).
- **Decisions**: the receipts: each push, its verdict, and the findings behind it.

## Filtering

The **filter rows** box live-filters the rows of the report currently shown: type a frame name, a repo, `rejected`, an author. It's client-side, disabled until you run a report, and clears when you run a new one. It only sees the rows already loaded, so widen **Rows** if you're filtering a big window.

## Reading the result

- A **truncation note** ("showing the newest N, older … not shown") appears when a report hits the Rows cap. Raise Rows, narrow the window, or scope to one repo to see the rest.
- An **:icon-warn: OBSERVE MODE** banner appears for a repo running `observe = true`. Its findings are recorded but pushes are never blocked (see the mode badge in the top bar, and [Policy](/policy) for how to switch to enforce).

## Reports vs Stats

Reports is window-based (7 / 30 / 90 / 365 days) over the full decision log. **[Stats](/stats)** is a focused per-repo view of time-saved + recurring findings with its own windows (all-time / 24h / 7d / 30d). They overlap on "time saved" and "recurring findings"; reach for **Stats** for at-a-glance repo health, and **Reports** for ad-hoc questions across any window or across all repos.

For the same data over MCP/REST: [docs/agent-api.md](https://github.com/nimblegate/nimblegate/blob/main/docs/agent-api.md).
