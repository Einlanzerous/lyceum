-- Per-user read marks are lost; books.finished_at, which this migration left
-- untouched, becomes the live column again with whatever it held before 0014.
DROP TABLE IF EXISTS book_reads;
