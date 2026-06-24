---
id: schema-vs-query-mismatch
description: Query shape incompatible with the underlying schema - wrong aggregation, wrong key, wrong table.
anticipated-siblings: []
---

# Pattern: schema-vs-query-mismatch

The query is valid SQL / GraphQL / DSL syntax. The schema accepts the query. The result is wrong because the query's assumptions don't match the schema's contract. Example: calling `count` on a pre-aggregated dataset that doesn't have row-level grain. Calling `sum(metric)` on an adaptive dataset where the metric is a max, not an additive quantity.

The structural defense: validate query shape against schema metadata before execution. The schema knows whether a field is summable, whether a dataset is pre-grouped, whether an index supports the requested join. Surface incompatibilities at write-time, not as silently-wrong numbers in production reports.
