// Negative: every column in POST_COLS exists in the paired schema.sql.
export const POST_COLS = ["id", "title", "slug"];

export function selectPosts(db) {
  return db.prepare(`SELECT ${POST_COLS.join(", ")} FROM posts`).all();
}
