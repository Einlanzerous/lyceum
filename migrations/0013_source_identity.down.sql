-- Drops the delete tombstones. The source_path NFC back-fill is not reversed:
-- the original mix of composed/decomposed spellings is not recorded anywhere,
-- and NFC paths are correct under the old code too (it compared them literally).
DROP TABLE IF EXISTS deleted_sources;
