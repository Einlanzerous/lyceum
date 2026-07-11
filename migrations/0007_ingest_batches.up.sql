-- LYCM-603: batch review of scanned ISBNs. A device (LYCM-602) uploads a batch
-- of scans; the server resolves each to a candidate (edition match + confidence)
-- that a desktop admin verifies and confirms before it joins inventory / the
-- library. Candidates are transient review state, distinct from the durable
-- inventory row a Confirm produces (migration 0003) — so they live in their own
-- tables and never pollute books/inventory until confirmed.

CREATE TABLE IF NOT EXISTS ingest_batches (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_device TEXT NOT NULL DEFAULT '',        -- e.g. "iPhone"; free-form label
    status        TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'confirmed', 'discarded')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ingest_candidates (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    batch_id          BIGINT NOT NULL REFERENCES ingest_batches(id) ON DELETE CASCADE,
    -- Canonical ISBN-13 for a resolved scan, or the raw read for a no_match so a
    -- malformed capture is surfaced for review rather than silently dropped.
    isbn              TEXT NOT NULL DEFAULT '',
    captured_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    source            TEXT NOT NULL DEFAULT 'camera'
        CHECK (source IN ('camera', 'manual', 'title')),
    status            TEXT NOT NULL DEFAULT 'review'
        CHECK (status IN ('ready', 'review', 'no_match', 'duplicate', 'confirmed', 'skipped')),
    confidence        DOUBLE PRECISION NOT NULL DEFAULT 0,   -- 0..1 match confidence
    chosen_edition_id TEXT NOT NULL DEFAULT '',              -- id within editions[], once picked
    editions          JSONB NOT NULL DEFAULT '[]'::jsonb,    -- candidate Edition[] from the resolver
    title             TEXT NOT NULL DEFAULT '',              -- resolved (chosen) metadata, for the queue
    author            TEXT NOT NULL DEFAULT '',
    cover_url         TEXT NOT NULL DEFAULT '',
    -- Series assignment intent (reuses the Series feature's fields); applied to
    -- the book once an EPUB for this title is ingested.
    series            TEXT NOT NULL DEFAULT '',
    series_index      DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ingest_candidates_batch_idx ON ingest_candidates (batch_id);
CREATE INDEX IF NOT EXISTS ingest_candidates_status_idx ON ingest_candidates (status);
