DROP INDEX IF EXISTS books_source_path_key;

ALTER TABLE books DROP COLUMN IF EXISTS source_path;
