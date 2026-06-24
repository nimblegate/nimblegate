-- Positive: bare RENAME COLUMN under migrations/. Re-run errors.
ALTER TABLE posts RENAME COLUMN old_title TO title;
