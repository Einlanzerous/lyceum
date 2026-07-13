-- LYCM-82: series intent captured at ingest confirm.
--
-- Series assigned in the verify UI was stored on the candidate only and never
-- reached the ingested book (which typically carries no OPF series metadata,
-- and arrives under the ebook ISBN rather than the scanned print ISBN). The
-- inventory row is the print↔ebook rendezvous point (LYCM-35), so the intent
-- is carried here: confirm stamps it, and ingest applies it to the book when
-- the EPUB itself declares no series.
ALTER TABLE inventory ADD COLUMN IF NOT EXISTS series TEXT;
ALTER TABLE inventory ADD COLUMN IF NOT EXISTS series_index DOUBLE PRECISION;
