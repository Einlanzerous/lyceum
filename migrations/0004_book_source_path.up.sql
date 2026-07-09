-- LYCM-66: stable identity for folder-ingested books.
--
-- source_path is the watched-tree path a book was ingested from (NULL for HTTP
-- uploads, which have no stable filesystem identity). It lets the folder watcher
-- REPLACE a re-stamped file's book in place — same row, same reading positions —
-- when the content hash changes, instead of ingesting it as a duplicate.
--
-- The unique index keeps one book per path; Postgres treats NULLs as distinct,
-- so the many upload rows with NULL source_path do not collide.
ALTER TABLE books ADD COLUMN IF NOT EXISTS source_path TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS books_source_path_key ON books (source_path);
