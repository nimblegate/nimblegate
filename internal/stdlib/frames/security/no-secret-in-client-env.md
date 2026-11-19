---
name: no-secret-in-client-env
category: security
subcategory: credentials
platform: []
framework: []
severity: BLOCK
severity-source: frame
tier: 1
dedup-key: file:line
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*"
pattern: secret-in-source
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 3/3
  last-run: 2026-11-19T12:00:00Z
---

# No secret in client env

Detect server-only secrets given a variable name that the framework copies
into the browser bundle. Vite (`VITE_`), Next.js (`NEXT_PUBLIC_`),
SvelteKit and Astro (`PUBLIC_`), Create React App (`REACT_APP_`), Expo
(`EXPO_PUBLIC_`) and Nuxt (`NUXT_PUBLIC_`) inline these values into the
JavaScript every visitor downloads - on purpose, so the frontend can read
them.

This is a common leak in AI-built apps: the agent names a key `VITE_...` so
a feature works from the browser, it works, and the key is now public.
`.env` being gitignored does not help, and neither does keeping the value
in the hosting dashboard: the build copies it either way. The key never
reaches git, but its name does - in the code that reads it and the config
that defines it - so the push can be stopped before the build ships it.

## What's detected

A client-prefixed name that is unambiguously server-only:

- contains `SECRET`, `PRIVATE`, `SERVICE_ROLE`, `PASSWORD`, `PASSWD` or
  `DATABASE_URL`, or
- names a server-only provider's key or token: `OPENAI`, `ANTHROPIC`,
  `RESEND`, `SENDGRID`, `TWILIO_AUTH`, `MAILGUN`, `POSTMARK` together with
  `KEY` or `TOKEN`.

Where it appears:

| Where | Example |
|---|---|
| Code that reads it | `import.meta.env.VITE_OPENAI_API_KEY`, `process.env.NEXT_PUBLIC_SUPABASE_SERVICE_ROLE_KEY`, `env["REACT_APP_..."]` |
| SvelteKit public env imports | `import { PUBLIC_STRIPE_SECRET } from '$env/static/public'` |
| Env files | `VITE_STRIPE_SECRET_KEY=` in `.env.example`, `.env.production` |
| Dockerfiles | `ARG VITE_STRIPE_SECRET_KEY`, `ENV NEXT_PUBLIC_JWT_SECRET=...` |
| CI, compose, TOML, JSON, framework config | `VITE_DATABASE_URL: ${{ secrets.DB }}`, `- NEXT_PUBLIC_JWT_SECRET=${JWT}` |

## Never fires

- Values meant for the browser: names containing `PUBLISHABLE`, `ANON`,
  `PUBLIC_KEY`, `SITE_KEY` or `MEASUREMENT_ID` (`VITE_STRIPE_PUBLISHABLE_KEY`,
  `PUBLIC_SUPABASE_ANON_KEY`, `NEXT_PUBLIC_RECAPTCHA_SITE_KEY`).
- Keys that are public by design and say nothing server-only, such as a maps
  or analytics key (`VITE_GOOGLE_MAPS_API_KEY`); restrict those by domain in
  the provider's console instead.
- Names without a client prefix (`OPENAI_API_KEY`), and SvelteKit's private
  modules (`$env/static/private`).
- Prose: Markdown, plain text and HTML, where tutorials name the variable to
  warn about it.
- Test files, `testdata/` and `fixtures/`, minified bundles and third-party
  code.

## Severity

`BLOCK`. Once a server-only name carries a client prefix, the build ships the
value to every visitor - there is nothing left to guess.

## Detection scope

- Triggers: `pre-commit`, `cli`.
- Applies to every scanned path except prose. Binary files and files over
  1 MiB are skipped; noise dirs (`node_modules/`, `dist/`) are excluded
  uniformly.

## Failure message

Only the variable name is reported, never a value.

```
✗ security/no-secret-in-client-env (security)
   server-only secret behind a browser-exposed env prefix:
   - src/chat.ts:3 - VITE_OPENAI_API_KEY is shipped to the browser
   fix: rename the variable without the client prefix and use it only in
        server code; if a build with the old name was deployed, ROTATE the key
```

## Override

Per-line, on the line above a value that is truly public:
```
// appframes:disable-next-line security/no-secret-in-client-env
```

Per-file:
```
// appframes:disable security/no-secret-in-client-env
```

## What's NOT detected

- **The key value itself** - a committed key with a known prefix (`sk-`,
  `sk_live_`) is `security/no-hardcoded-credentials`; a committed `.env` is
  `security/no-env-file-in-repo`.
- **Ambiguous names** - `VITE_API_KEY`, `NEXT_PUBLIC_TOKEN`. Many such keys
  are public; check them by hand, or enable `security/possible-secret`.
- **Keys passed to the browser some other way** - a server route that returns
  the key in JSON, or a key pasted into frontend code under any name.
