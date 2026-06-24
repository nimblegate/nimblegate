-- Negative: CREATE TABLE IF NOT EXISTS is natively idempotent.
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY,
  email TEXT NOT NULL
);
