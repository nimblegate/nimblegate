---
id: unsanitized-data-flow
description: User-controlled input flowing to a dangerous sink without sanitization at the boundary.
anticipated-siblings: []
---

# Pattern: unsanitized-data-flow

User input ends up as the second argument to `.innerHTML`, interpolated into a SQL string, passed to `exec()`, used as a file path without basename checks. Each sink has a known-safe encoding (text content, parameterized query, argument array, basename-only). When code skips the safe encoding, the input controls the sink.

The structural defense: identify the dangerous sinks per language/framework, identify the user-input sources, and refuse paths from source to sink that don't pass through a sanitization layer. Static analysis can do this approximately; type system encoding can do it more strictly. Either beats "we'll be careful."
