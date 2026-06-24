---
name: cf-graphql-schema-match
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
runs-after: [app-correctness/cf-graphql-dataset-by-window]
pattern: schema-vs-query-mismatch
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 2/2
  negatives: 2/2
  last-run: 2026-05-20T14:37:33Z
---

# app-correctness/cf-graphql-schema-match

Reject CF GraphQL queries that select fields the dataset doesn't expose. The two failure modes are symmetric:

| Wrong combination | What CF returns | Fix |
|-------------------|-----------------|-----|
| `count` on `httpRequests1hGroups` / `1dGroups` | Generic field-doesn't-exist error | Switch to `sum { requests }` |
| `sum.requests` on `httpRequestsAdaptiveGroups` | Generic field-doesn't-exist error | Switch to `count` |

Catches the same class of footgun as `app-correctness/cf-graphql-dataset-by-window` but for the **schema** axis rather than the **time window** axis. The two compose: one rules out wrong-dataset-for-window, the other rules out right-dataset-wrong-fields.

## Schema cheat sheet

| Dataset family | Valid top-level fields | Invalid (will fail) |
|----------------|------------------------|---------------------|
| `httpRequestsAdaptiveGroups`, `httpRequestsAdaptive` | `count`, `dimensions`, `avg` | `sum` |
| `httpRequests1hGroups`, `httpRequests1mGroups`, `httpRequests1dGroups` | `sum.{requests,bytes,cachedRequests,...}`, `uniq.uniques`, `dimensions`, `avg` | `count` |

Note that `dimensions`, `avg` are valid in both - the asymmetry is `count` vs `sum`.

## Fix

```graphql
# WRONG - count on 1dGroups doesn't exist
httpRequests1dGroups(filter: { date_geq: "2026-05-11" date_leq: "2026-05-18" }) {
  count
}

# RIGHT
httpRequests1dGroups(filter: { date_geq: "2026-05-11" date_leq: "2026-05-18" }) {
  sum { requests }
}
```

```graphql
# WRONG - sum on Adaptive doesn't exist
httpRequestsAdaptiveGroups(filter: { datetime_geq: "..." datetime_leq: "..." }) {
  sum { requests }
}

# RIGHT
httpRequestsAdaptiveGroups(filter: { datetime_geq: "..." datetime_leq: "..." }) {
  count
}
```

## Suppressing intentional cases

For one-off historical queries explicitly using a non-standard field combination (e.g. on a paid tier that exposes additional fields):

```graphql
# appframes:disable-next-line app-correctness/cf-graphql-schema-match
httpRequestsAdaptiveGroups(...) {
  sum { requests }   # custom paid-tier extension
}
```

## Generalizes to

Any analytics API with **multiple datasets with overlapping but non-identical schemas**:

- GA4 dimensions vs metrics per report type
- Datadog query types (metrics vs events vs logs)
- BigQuery views over underlying tables (column subset can change)
- Snowflake share schemas

The frame currently codifies Cloudflare's two main dataset families. Adding others requires populating `cfDatasetSchemas` with the equivalent valid/invalid field maps.
