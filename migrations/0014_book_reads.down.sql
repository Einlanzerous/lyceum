-- Per-user read marks are lost; books.finished_at becomes the live column again
-- with whatever it holds at the time.
--
-- Run 0015's down first if 0015 has been applied. 0015 dropped that column, and
-- its down is what re-adds it and refills it from the owner's reads; this one
-- run alone after 0015 drops book_reads, the last surviving copy of those
-- marks, and every book comes back unread.
DROP TABLE IF EXISTS book_reads;
