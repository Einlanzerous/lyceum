-- LYCM-112: "read" becomes yours rather than the library's.
--
-- books.finished_at (0006) predates accounts by five migrations, so mark-as-read
-- stayed a property of the book when 0011 made reading positions a property of
-- the reader. One housemate finishing a volume showed it read for everyone —
-- their ✓ Read pill, their series progress, and since LYCM-108 their pinned
-- "continue reading" slot, which a book they never opened could empty.
--
-- The row keys on (book, user), not reading_positions' (book, user, device):
-- finishing a book is something a person did, not something a device holds.
CREATE TABLE IF NOT EXISTS book_reads (
    book_id     BIGINT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- A row exists only for a book someone has marked read, and un-marking
    -- deletes it: NOT NULL keeps "finished" and "row present" the same fact.
    finished_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (book_id, user_id)
);

-- Everything marked read before accounts existed was read by the single
-- pre-accounts user, who is now the owner — the same call 0011 made for
-- reading positions, and the only defensible guess.
INSERT INTO book_reads (book_id, user_id, finished_at)
SELECT b.id, u.id, b.finished_at
  FROM books b, users u
 WHERE b.finished_at IS NOT NULL
   AND u.is_owner
ON CONFLICT (book_id, user_id) DO NOTHING;

-- books.finished_at is left in place, unread by the application from here on.
-- Dropping it is a follow-up (LYCM-114), so that a rollback to the previous
-- binary still finds the marks it wrote.
