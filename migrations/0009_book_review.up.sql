-- LYCM-58: ingestion QC review queue. A newly-ingested book that trips a
-- detector (no ISBN, no/poor-source cover, mangled title) lands pending-review
-- and is held off the shelf until a human approves / edits / replaces its cover.
-- Clean ingests publish straight through, so existing rows and normal imports
-- default to 'published' and shelf behavior is unchanged when QC is off.
--
--   * review_state — 'published' (on the shelf) or 'pending' (awaiting review).
--   * review_flags — the detected issue codes, e.g. ["no_isbn","suspicious_title"].

ALTER TABLE books ADD COLUMN IF NOT EXISTS review_state TEXT NOT NULL DEFAULT 'published';
ALTER TABLE books ADD COLUMN IF NOT EXISTS review_flags JSONB NOT NULL DEFAULT '[]';

-- Partial index for the (usually small) pending queue, newest first.
CREATE INDEX IF NOT EXISTS books_pending_idx ON books (added_at DESC)
    WHERE review_state <> 'published';
