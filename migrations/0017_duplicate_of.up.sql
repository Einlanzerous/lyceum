-- LYCM-113: record which book a held ingest looks like a second copy of.
--
-- Book identity is per file — file_hash is unique and source_path is unique — so
-- two EPUBs of one work are two rows as long as the bytes differ, and the shelf
-- shows the book twice. Ingest now looks for an existing book of the same work
-- and holds the newcomer in the review queue instead, carrying a pointer to what
-- it matched.
--
-- The pointer is a column rather than another review_flags entry because it is
-- an id, not an issue code: review_flags is a set of stable strings the web maps
-- to labels, and packing "possible_duplicate:42" into one would make every
-- reader of that array parse it. The flag still goes in review_flags; this says
-- what it is about.
--
-- ON DELETE SET NULL, not CASCADE: resolving a duplicate by deleting the older
-- book must not delete the newer one along with it. The flag outliving its
-- pointer is the right failure — the review UI falls back to "the book this
-- matched is gone", which is exactly what happened.
ALTER TABLE books ADD COLUMN IF NOT EXISTS duplicate_of BIGINT
    REFERENCES books(id) ON DELETE SET NULL;

-- Deleting a book has to find the rows pointing at it to null them out, and an
-- unindexed referencing column makes that a sequential scan of the shelf.
CREATE INDEX IF NOT EXISTS books_duplicate_of_idx
    ON books (duplicate_of) WHERE duplicate_of IS NOT NULL;
