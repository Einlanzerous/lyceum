-- LYCM-115 turned the shelf's per-user lookups into two batch queries, each
-- filtering on user_id: every position one reader holds, and every book they
-- have marked read. Neither table had an index that could serve that.
--
-- Both existing indexes lead with book_id — reading_positions' UNIQUE is
-- (book_id, user_id, device_id) and book_reads' primary key is (book_id,
-- user_id) — and a B-tree is only usable from its leading column, so a bare
-- user_id filter sequentially scans both. The old per-book queries matched those
-- indexes exactly, which is why nothing needed this before.
--
-- Second, independent reason: both user_id columns are REFERENCES users(id) ON
-- DELETE CASCADE with no index behind them, so removing a housemate scanned
-- both tables to find the rows to cascade.

-- (user_id, book_id), not user_id alone. The rest of the batch query's ORDER BY
-- is progress and recency, which no index here can supply, so a sort stays in
-- the plan either way — but book_id is its leading key, and carrying it as the
-- second column lets that sort become an incremental one over presorted groups
-- rather than a full sort of the reader's history. Measured on 10k positions:
-- with the second column the ordered plan is an Incremental Sort with
-- "Presorted Key: book_id"; with user_id alone it is a full Sort. At household
-- size the planner still prefers a bitmap scan and a full sort, which is
-- already ~6x fewer buffers than the sequential scan this replaces; the second
-- column is what keeps the better plan available as a history grows.
CREATE INDEX IF NOT EXISTS reading_positions_user_book_idx
    ON reading_positions (user_id, book_id);

-- book_reads is read whole for one user and returns bare ids, so user_id alone
-- covers it; the primary key still serves the single-book membership test.
CREATE INDEX IF NOT EXISTS book_reads_user_idx
    ON book_reads (user_id);
