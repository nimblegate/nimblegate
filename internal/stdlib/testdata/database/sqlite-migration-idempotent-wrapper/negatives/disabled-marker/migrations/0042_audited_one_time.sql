-- Negative: explicit IDEMPOTENT-WRAPPER-NOT-REQUIRED inline opt-out.
-- One-time historical migration audited by hand.
-- IDEMPOTENT-WRAPPER-NOT-REQUIRED
ALTER TABLE users ADD COLUMN email_verified_at TEXT;
