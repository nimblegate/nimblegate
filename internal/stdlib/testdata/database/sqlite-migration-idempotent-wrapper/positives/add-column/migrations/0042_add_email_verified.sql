-- Positive: bare ALTER TABLE ADD COLUMN inside a migrations/ directory
-- (required by the frame's applicable-file rule). Re-run errors with
-- "duplicate column".
ALTER TABLE users ADD COLUMN email_verified_at TEXT;
