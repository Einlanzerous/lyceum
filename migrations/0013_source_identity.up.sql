-- LYCM-109: two fixes to book lifecycle identity.
--
-- 1. source_path is stored in Unicode NFC. The acquisition pipeline re-imported
--    "Victor Milán" with á decomposed (a + U+0301) where the first import used
--    the composed form, and those distinct byte strings read as two different
--    paths — so the same book ingested twice. Ingest now normalizes; this
--    back-fills the rows written before it did.
--
--    Rows whose normalized form is already claimed by another row are left
--    alone: those are the duplicates this migration cannot merge (it would trip
--    books_source_path_key), and GetBookBySourcePath normalizes at query time
--    so they still resolve to the lowest-id row. row_number() keeps a group of
--    several un-normalized rows from colliding with each other mid-statement.
WITH canon AS (
    SELECT id,
           normalize(source_path, NFC) AS np,
           row_number() OVER (PARTITION BY normalize(source_path, NFC) ORDER BY id) AS rn
      FROM books
     WHERE source_path IS NOT NULL
)
UPDATE books b
   SET source_path = c.np
  FROM canon c
 WHERE b.id = c.id
   AND c.rn = 1
   AND b.source_path <> c.np
   AND NOT EXISTS (SELECT 1 FROM books o WHERE o.id <> b.id AND o.source_path = c.np);

-- 2. Deleting a folder-ingested book has to outlive the watcher's in-memory
--    "already seen" map, or the next restart re-ingests the file still sitting
--    in the watched tree and the book comes back. A tombstone records the
--    deletion by both identities the watcher matches on: the content hash (same
--    bytes, wherever they land) and the source key (any bytes at the deleted
--    book's path, i.e. a re-stamp). Lyceum never deletes from the watched tree
--    itself — that media belongs to the acquisition stack.
CREATE TABLE IF NOT EXISTS deleted_sources (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- lower(normalize(source_path, NFC)); '' for books that were uploaded
    -- rather than folder-ingested, which tombstone on hash alone.
    source_key TEXT NOT NULL DEFAULT '',
    file_hash  TEXT NOT NULL UNIQUE,
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS deleted_sources_source_key
    ON deleted_sources (source_key) WHERE source_key <> '';
