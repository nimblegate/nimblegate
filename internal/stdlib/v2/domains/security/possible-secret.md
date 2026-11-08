---
name: possible-secret
category: security
subcategory: credentials
platform: []
framework: []
severity: WARN
severity-source: frame
tier: 2
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
  positives: 2/2
  negatives: 4/4
  last-run: 2026-11-08T10:12:41Z
---

# Possible secret

Warn about values that are probably secrets but cannot be proven to be.
Prefixed keys are blocked by `security/no-hardcoded-credentials` and
passwords in telling shapes by `security/no-plaintext-passwords`; this frame
covers the rest with two heuristics, and only ever warns.

## What's detected

- **A credential-named field holding a real-looking literal.** The name's
  last word is `password`, `passwd`, `pwd`, `passphrase`, `secret`, `token`
  or `apikey`, or its last two are `api key`, `secret key`,
  `access key`, `private key`, `auth token`, `access token`, `refresh token`
  or `client secret` - so `dbPassword`, `SECRET_KEY`, `apiKey` and
  `client_secret` count, while `tokenizer`, `publicKey` or a lone `key` do
  not. The value must be a quoted literal of 8+ characters with a digit or
  capital letter, mixing at least two character classes (unquoted values count in YAML, INI, TOML and
  `.properties` files).
- **A high-entropy string near a secret keyword.** A quoted literal (or a
  `Bearer` value) of 20+ characters with lowercase letters, at least two
  capitals and two digits, and at least 3.5 bits of entropy per character
  (separator-joined words such as header names or dotted keys never count),
  on a line - or just below a
  line - mentioning a secret, token, password, credential, bearer or
  authorization.

## Never fires

- Environment lookups and references (`os.environ[...]`, `process.env.X`,
  `${VAR}`), constant names (`DB_PASSWORD`), placeholders and marked fakes.
- Values naming the field (`"password_confirmation"`), single-class values
  (`"12345678"`).
- UUIDs, hex hashes, `sha256-`/`sha512-` integrity values, data URIs, and
  any line mentioning a hash, digest, checksum or integrity.
- Test files, `testdata/`, `fixtures/`, lockfiles, minified bundles, source
  maps, lines over 500 characters, and third-party code (`vendor/`,
  `third_party/`, `.venv/`, `site-packages/`, `.svelte-kit/`).
- Character sets such as `ABCDEFGHIJ...0123456789` in token generators, and a
  keyword inside a longer word (`tokenizer`, `AutoTokenizer`).
- Lines already caught by `security/no-hardcoded-credentials`.

## Severity

`WARN`. These are guesses; a guess must never stop a push. Check each one:
move real values to an environment variable or secret manager, and mark
fakes so they stop warning.

## Detection scope

- Triggers: `pre-commit`, `cli`.
- Applies to every scanned path except the skipped kinds above. Binary
  files and files over 1 MiB are skipped.

## Failure message

The reason names the file, line and field - values are **never echoed**.

```
⚠ security/possible-secret (security)
   possible secrets (values redacted; check each - a guess, not proof):
   - config/prod.yaml:12 - possible secret in password
   - src/billing.ts:40 - high-entropy string near a secret keyword
```

## Override

Per-line, on the line above a known-fake or public value:
```
# appframes:disable-next-line security/possible-secret
```

Per-file:
```
# appframes:disable security/possible-secret
```

## What's NOT detected

- **Secrets with no telling name or context** - a random string assigned to
  `x` with no secret word nearby. Detecting those would mean flagging every
  random-looking string.
- **Short or low-entropy secrets** outside a credential-named field.
