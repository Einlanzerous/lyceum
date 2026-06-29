-- LYCM-601: inventory / ownership state, keyed by ISBN and decoupled from books.
--
-- A books row requires an on-disk EPUB (file_path / file_hash are NOT NULL), so
-- it cannot represent a title you own physically but have not yet acquired as a
-- file. Inventory carries that ownership/acquisition state independently and
-- links to a book once one is ingested (book_id), keeping ISBN out of books to
-- avoid two sources of truth.

CREATE TABLE IF NOT EXISTS inventory (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    isbn              TEXT NOT NULL UNIQUE,           -- canonical ISBN-13 (digits only)
    title             TEXT,
    author            TEXT,
    acquisition_state TEXT NOT NULL DEFAULT 'owned'
        CHECK (acquisition_state IN ('owned', 'wanted', 'acquiring', 'ingested')),
    -- The ingested EPUB, once one exists. SET NULL (not CASCADE) so deleting a
    -- book drops the link but keeps the ownership record.
    book_id           BIGINT REFERENCES books(id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS inventory_book_id_idx ON inventory (book_id);
CREATE INDEX IF NOT EXISTS inventory_state_idx ON inventory (acquisition_state);
