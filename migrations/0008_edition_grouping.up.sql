-- LYCM-35 part 2: group print & ebook editions of one work onto a single
-- inventory title. A scan yields the *print* ISBN; the acquired EPUB carries a
-- different *ebook* ISBN for the same work, so keying inventory 1:1 on the ISBN
-- (migration 0003) produced two unlinked rows. Two additions reconcile them:
--
--   * work_id — the OpenLibrary work key the edition resolver returns for either
--     edition. A print scan and an ebook ingest that resolve to the same work
--     meet on one row.
--   * inventory_isbns — every ISBN (print + ebook) known to map to an entry, so
--     a lookup by *either* number finds the same row. inventory.isbn stays the
--     primary/first-seen code; this table is the full set.

ALTER TABLE inventory ADD COLUMN IF NOT EXISTS work_id TEXT; -- OpenLibrary work key, e.g. /works/OL20125776W
CREATE INDEX IF NOT EXISTS inventory_work_id_idx ON inventory (work_id);

CREATE TABLE IF NOT EXISTS inventory_isbns (
    isbn13       TEXT PRIMARY KEY,   -- canonical ISBN-13; an ISBN maps to exactly one entry
    inventory_id BIGINT NOT NULL REFERENCES inventory(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS inventory_isbns_inventory_id_idx ON inventory_isbns (inventory_id);

-- Backfill: each existing entry's primary ISBN becomes a known ISBN.
INSERT INTO inventory_isbns (isbn13, inventory_id)
SELECT isbn, id FROM inventory
ON CONFLICT (isbn13) DO NOTHING;
