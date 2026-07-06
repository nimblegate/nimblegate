---
id: pii-in-source
description: Real personal data (card numbers, bank accounts, national IDs) committed to a repository.
anticipated-siblings: []
---

# Pattern: pii-in-source

Test fixtures, seed data, debug dumps, CSV exports - the routine places real
customer records leak into git. Unlike a leaked API key, personal data cannot
be rotated: once a card number or national ID reaches a remote, the exposure
is permanent and, in regulated industries, reportable.

AI agents widen the funnel: an agent asked to "write a test with realistic
data" may copy records from a connected database or a production log instead
of inventing fakes, and the diff looks exactly like a normal fixture.

The structural defense: recognise the formats that validate - payment card
numbers pass Luhn, IBANs pass mod-97, national IDs follow strict shapes - and
flag them at push time, while excluding the well-known publishable test values
(Stripe test cards, documentation IBANs) that are fake by design. Checksum
validation is what separates this from noisy digit-matching: a random number
almost never validates.
