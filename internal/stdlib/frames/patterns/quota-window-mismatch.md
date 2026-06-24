---
id: quota-window-mismatch
description: Querying a window or scope larger than the underlying system's retention / quota allows.
anticipated-siblings: []
---

# Pattern: quota-window-mismatch

Free-tier Cloudflare Analytics retains 31 days. The query asks for 90 days. The response truncates to 31 days; the caller assumes 90. Reports plot strange shapes; nobody notices the early data is missing. Same shape: rate-limited APIs asked for batches exceeding the limit, paginated endpoints asked for offsets beyond the cap.

The structural defense: encode the system's limits in metadata and validate at query-construction time. If the user asks for 90 days against a 31-day source, either reject the query or surface a loud warning that the response will be truncated. Silent truncation is the failure mode; explicit rejection is the cheap fix.
