---
name: cf-graphql-dataset-by-window
category: app-correctness
subcategory: data-fetching
platform: [cloudflare]
framework: []
severity: WARN
tier: 3
tags: [cloudflare, cf-analytics, graphql, content-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/*.graphql"
    - "**/*.gql"
    - "**/n8n/**/*.json"
    - "**/workflows/**/*.json"
dedup-key: "file:line"
pattern: quota-window-mismatch
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 2/2
  last-run: 2026-05-20T14:37:33Z
---

# app-correctness/cf-graphql-dataset-by-window

Catch Cloudflare Analytics GraphQL queries whose `datetime_geq` / `date_geq` range exceeds the queried dataset's Free-tier retention cap. CF rejects these silently - with an error that doesn't point at the dataset choice as the cause - so the first sign is usually a workflow that "just stopped working" after a date range was bumped. The typical failure mode is multiple edit + redeploy cycles to find the right dataset, because each rejection produces a different generic error that doesn't name the dataset cap as the cause.

## Free-tier dataset retention

| Dataset | Cap | Notes |
|---------|-----|-------|
| `httpRequestsAdaptiveGroups` | 1 day | `count` schema; finest-grain |
| `httpRequestsAdaptive` | 1 day | Singular form |
| `httpRequests1hGroups` | 3 days | `sum.requests` schema |
| `httpRequests1mGroups` | 3 days | Minute-grain (rarely useful at scale) |
| `httpRequests1dGroups` | 30 days | Daily aggregate; widest window |
| `firewallEventsAdaptiveGroups` | 1 day | Firewall events |

## What this catches

For each dataset reference in a CF GraphQL query, the analyzer:
1. Finds the surrounding `datetime_geq` / `datetime_leq` / `date_geq` / `date_leq` arguments
2. Parses the timestamps (ISO 8601 or YYYY-MM-DD)
3. Computes the span between earliest and latest values
4. Compares against the dataset's retention cap
5. BLOCKs if the span exceeds the cap

Files scanned: `.graphql`, `.gql`, and JSON files under `**/n8n/**` or `**/workflows/**`.

## Fix

Switch to a dataset whose retention covers the window. **Each dataset has a different schema** - you can't just change the name:

```graphql
# WRONG - Adaptive caps at 1 day; this query asks for 7 days
{
  viewer {
    accounts(filter: { accountTag: $accountTag }) {
      httpRequestsAdaptiveGroups(
        limit: 1000
        filter: { datetime_geq: "2026-05-11T00:00:00Z" datetime_leq: "2026-05-18T00:00:00Z" }
      ) {
        count   # ← Adaptive schema
      }
    }
  }
}

# RIGHT - 1dGroups for 7d (30d retention); schema is `sum`
{
  viewer {
    accounts(filter: { accountTag: $accountTag }) {
      httpRequests1dGroups(
        limit: 1000
        filter: { date_geq: "2026-05-11" date_leq: "2026-05-18" }
      ) {
        sum { requests }   # ← 1d schema
      }
    }
  }
}
```

Note the schema differences:
- Adaptive uses `count`
- 1h / 1d use `sum.{requests,bytes,cachedRequests,...}`
- Field names like `cachedRequests` exist on some datasets, not others

## Suppressing intentional cases

For one-off historical queries explicitly using a dataset over its cap (e.g. on a paid tier with extended retention):

```graphql
# appframes:disable-next-line app-correctness/cf-graphql-dataset-by-window
httpRequestsAdaptiveGroups(filter: { datetime_geq: "2026-04-01T00:00:00Z" datetime_leq: "2026-05-18T00:00:00Z" })
```

## Generalizes to

Any analytics surface with **multiple datasets covering different time windows**, where the wrong choice produces opaque errors:

- BigQuery free-tier byte caps (different from time, but same shape: pick the dataset that fits the query)
- Mixpanel / Amplitude data export windows
- Segment historical replay caps
- Datadog metrics retention tiers
- New Relic event vs metric retention

The frame currently covers Cloudflare's named datasets only. Adding others requires populating the equivalent retention table.
