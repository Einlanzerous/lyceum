-- Series roll-up (LYCM-36): books carry an optional series name and position so
-- the library can group multi-book series under one card. Read from EPUB OPF
-- metadata (calibre:series / belongs-to-collection); both are nullable because
-- most standalone books declare neither.
ALTER TABLE books ADD COLUMN IF NOT EXISTS series       TEXT;
ALTER TABLE books ADD COLUMN IF NOT EXISTS series_index DOUBLE PRECISION;

-- Grouping and reading-order sorts scan by series; index it for larger shelves.
CREATE INDEX IF NOT EXISTS books_series_idx ON books (series);
