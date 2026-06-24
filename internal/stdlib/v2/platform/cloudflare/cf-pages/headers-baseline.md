---
name: cf-pages-headers-baseline
category: security
subcategory: headers
platform: [cloudflare, cf-pages]
framework: []
severity: WARN
tier: 2
tags: [cloudflare, cf-pages, security-headers, content-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/_headers"
pattern: missing-standard-protection
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
  last-run: 2026-05-20T14:37:33Z
---

# security/cf-pages-headers-baseline

When a CF Pages `_headers` file is present, surface a WARN for each missing baseline security header:

| Header | Why |
|--------|-----|
| `Content-Security-Policy` | Primary XSS / data-exfiltration defense |
| `X-Frame-Options` | Clickjacking protection (or `frame-ancestors` directive in CSP) |
| `X-Content-Type-Options` | Prevents MIME-sniff attacks; should be `nosniff` |
| `Referrer-Policy` | Controls what Referer header value goes on outgoing requests |
| `Strict-Transport-Security` | HSTS - pins HTTPS for the host (and optionally subdomains) |

When no `_headers` file exists in the project, PASS. The frame doesn't enforce "you must have a _headers file" - that's a per-project decision. It only enforces "if you have one, the baseline must be present."

## Fix

Minimal baseline under `/*` in your `_headers` file (CF Pages convention):

```
/*
  Content-Security-Policy: default-src 'self'
  X-Frame-Options: DENY
  X-Content-Type-Options: nosniff
  Referrer-Policy: strict-origin-when-cross-origin
  Strict-Transport-Security: max-age=31536000; includeSubDomains
```

You almost certainly want a stricter CSP than `default-src 'self'`. Build it up incrementally:

```
/*
  Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self'; connect-src 'self' https://api.your-domain.com; frame-ancestors 'none'
```

The CSP-builder + CSP-evaluator tools at <https://csp-evaluator.withgoogle.com/> are useful for iterating on this without trial-and-error in production.

## Header scope

CF Pages `_headers` syntax supports per-route headers:

```
/*
  X-Frame-Options: DENY

/api/*
  Cache-Control: no-store

/static/*
  Cache-Control: public, max-age=31536000, immutable
```

This frame's check is loose on routing - declaring a baseline header for ANY route satisfies the baseline. Project-specific policies that want "every route must declare X-Frame-Options" can suppress this frame and add their own stricter check.

## Suppressing intentional cases

For projects with a custom security-headers strategy (e.g. Workers middleware that sets them dynamically), suppress at file level:

```
# appframes:disable security/cf-pages-headers-baseline
# Headers set by src/hooks.server.ts; _headers is only for static-asset caching.

/static/*
  Cache-Control: public, max-age=31536000, immutable
```

## Generalizes to

Any platform with a static `_headers`-style config:

- Cloudflare Pages - covered
- Netlify `_headers` files - same syntax; this frame would work if scoped to those projects
- Vercel `vercel.json` headers - different syntax; not covered here
- Apache `.htaccess` / Nginx `add_header` directives - different syntax; not covered

Adding Vercel / Netlify is a future refinement; the regex would need adjusting per format.
