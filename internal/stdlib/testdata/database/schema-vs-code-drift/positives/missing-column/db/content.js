// Positive: POST_COLS lists "ghost_column" which doesn't exist in the
// paired schema.sql - silent drift between declared columns and code.
export const POST_COLS = ["id", "title", "slug", "ghost_column"];

export function selectPosts(db) {
  return db.prepare(`SELECT ${POST_COLS.join(", ")} FROM posts`).all();
}
