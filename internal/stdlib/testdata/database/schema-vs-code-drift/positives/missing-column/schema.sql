CREATE TABLE posts (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE
);
-- Note: ghost_column referenced in db/content.js is NOT defined here.
