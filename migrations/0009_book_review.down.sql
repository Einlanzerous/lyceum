DROP INDEX IF EXISTS books_pending_idx;
ALTER TABLE books DROP COLUMN IF EXISTS review_flags;
ALTER TABLE books DROP COLUMN IF EXISTS review_state;
