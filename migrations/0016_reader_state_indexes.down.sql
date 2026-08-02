-- Dropping these costs no data: they are read paths only. The batch queries in
-- LYCM-115 fall back to sequential scans, which is what they did before 0016.
DROP INDEX IF EXISTS reading_positions_user_book_idx;
DROP INDEX IF EXISTS book_reads_user_idx;
