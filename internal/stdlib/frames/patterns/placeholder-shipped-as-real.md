---
id: placeholder-shipped-as-real
description: Temporary / development content reaching production - lorem ipsum, localhost URLs, FIXME markers, sample data.
anticipated-siblings: []
---

# Pattern: placeholder-shipped-as-real

A designer dropped lorem ipsum in for layout. An engineer hardcoded `localhost:3000` for testing. A QA stub left `INSERT REAL TEXT HERE` in the page. The placeholder served its purpose in dev - and nobody remembered to swap it before ship. The page goes live with "Acme Customer" or "Sample Address 123."

The structural defense: catalog the placeholder patterns (lorem ipsum, localhost references, FIXME/TODO:ship markers, sample-data sentinels) and refuse them in production-bound artifacts. The check has zero false positives in real flows - these strings shouldn't appear in shipping content - and prevents the most public class of release embarrassment.
