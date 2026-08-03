-- Drops the suspected-duplicate pointers. The possible_duplicate entries in
-- review_flags are left alone: they are still true statements about why a book
-- is held, and the review UI degrades to showing the flag without a counterpart.
DROP INDEX IF EXISTS books_duplicate_of_idx;
ALTER TABLE books DROP COLUMN IF EXISTS duplicate_of;
