-- Negative: SELECT statement, not DDL.
SELECT id, email FROM users WHERE email_verified_at IS NOT NULL;
