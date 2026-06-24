---
id: missing-standard-protection
description: Recognized config or page type missing standard protective elements that the platform / spec expects.
anticipated-siblings: []
---

# Pattern: missing-standard-protection

A `_headers` file without CSP. An HTML page without `<meta charset>`. An `<img>` without `alt=`. A `dockerfile` without `USER`. Each is a recognized artifact with a known set of protective elements; each instance ships missing some of them. The omissions accumulate as the project grows and nobody re-audits.

The structural defense: list the recognized artifacts (by name, extension, or content shape) and the protective elements each one should carry. Surface missing elements as warnings - not blocks, since some are project-specific judgment calls - with a clear "here's what's missing and why it matters" message. Cheap to add; widely applicable.
