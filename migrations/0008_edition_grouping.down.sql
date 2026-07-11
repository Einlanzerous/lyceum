DROP TABLE IF EXISTS inventory_isbns;
DROP INDEX IF EXISTS inventory_work_id_idx;
ALTER TABLE inventory DROP COLUMN IF EXISTS work_id;
