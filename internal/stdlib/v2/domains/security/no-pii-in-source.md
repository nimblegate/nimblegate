---
name: no-pii-in-source
category: security
subcategory: content-safety
platform: []
framework: []
severity: WARN
tier: 2
dedup-key: file:line
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*"
pattern: pii-in-source
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 4/4
  negatives: 4/4
  last-run: 2026-07-06T05:45:26Z
---

# No PII in source

Detect real-looking personal data committed to the repository - payment card
numbers, bank account numbers (IBAN), and national IDs in fixtures, seed
data, logs, and CSV exports. Unlike a leaked credential, personal data cannot
be rotated: once pushed to a remote the exposure is permanent, and in
regulated industries it is reportable.

## What's detected

Every detector is **checksum- or shape-validated** so random digit runs
almost never fire:

| Kind | Validation |
|---|---|
| Payment card number | 13-19 digits (spaces/dashes tolerated), known scheme prefix (Visa, Mastercard, Amex, Discover, JCB), passes Luhn, and is NOT a well-known publishable test number (Stripe/docs test cards report nothing) |
| IBAN | Country code with the correct national length, passes mod-97, and is NOT a well-known documentation example |
| US SSN | `NNN-NN-NNNN` shape with valid area/group/serial, only when the line carries identifying context (`ssn`, `social security`, `tax id`, `taxpayer`) |

## Severity

`WARN`. Fixtures containing deliberately fake-but-valid numbers exist in the
wild; the operator decides per repo whether to escalate to `BLOCK` via
severity tuning. Known publishable test values are excluded entirely rather
than downgraded - they are fake by design and reporting them is noise.

## Detection scope

- Triggers: `pre-commit`, `cli`.
- Applies to every scanned file: personal data hides in `.sql`, `.csv`,
  `.json`, source code, and logs alike. Binary files (NUL byte in content)
  and files over 1 MiB are skipped; noise dirs (`node_modules/`, `dist/`)
  are excluded uniformly.

## Failure message

The reason names file, line, and kind - but **never echoes the matched
value**; the audit log must not re-leak the data.

```
⚠ security/no-pii-in-source (security)
   possible personal data detected (raw values redacted):
   - db/seed/customers.csv:12 - payment card number (Luhn-valid)
   - tests/fixtures/accounts.sql:3 - IBAN (checksum-valid)
   fix: replace real records with clearly-fake fixtures (Stripe test cards,
        documentation IBANs); if real customer data was committed, scrub
        history and treat as an incident
```

## Override

Per-file disable:
```
# appframes:disable security/no-pii-in-source
```

Per-line disable (suppresses the line that follows the marker):
```sql
-- appframes:disable-next-line security/no-pii-in-source
INSERT INTO cards VALUES ('4485275742300001');
```

Use per-line only for values verified fake. When in doubt, treat as real.

## What's NOT detected

- **Names, emails, dates of birth, addresses** - no validating structure;
  matching them is noise, not detection.
- **Phone numbers** - format overlaps timestamps and IDs too heavily.
- **National IDs beyond the US SSN** - shapes vary per country; expansion
  candidate once per-country validators exist.
- **Card numbers split across lines** or stored digit-by-digit - line-scoped
  matching only.
