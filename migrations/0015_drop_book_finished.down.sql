-- Restores 0006's column and refills it from book_reads, the mirror of 0014's
-- back-fill: the owner's reads are what a library-wide flag meant before
-- accounts, so they are what it means again on the way back.
--
-- Only the owner's. Folding every housemate's reads into one shared flag would
-- hand each of them the others' books, the exact bug LYCM-112 fixed; a pre-112
-- binary rolled back this far should see the shelf it last saw.
--
-- Run this before 0014's down, which drops book_reads and expects to find the
-- column already carrying those marks.
ALTER TABLE books ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ;

UPDATE books b
   SET finished_at = r.finished_at
  FROM book_reads r, users u
 WHERE r.book_id = b.id
   AND r.user_id = u.id
   AND u.is_owner;
