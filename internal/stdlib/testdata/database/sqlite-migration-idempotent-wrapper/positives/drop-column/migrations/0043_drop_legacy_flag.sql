-- Positive: bare DROP COLUMN under migrations/. Same idempotency footgun.
ALTER TABLE posts DROP COLUMN legacy_flag;
