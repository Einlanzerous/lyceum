-- LYCM-402: "Send to Kindle" delivery tracking.
--
-- One row per delivery attempt for a book. status moves queued -> sent | failed
-- as the async worker (internal/api dispatcher) processes the job; error holds
-- the failure detail when status = 'failed'.

CREATE TABLE IF NOT EXISTS deliveries (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    book_id    BIGINT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    to_addr    TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'queued',
    error      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS deliveries_book_id_idx ON deliveries (book_id, created_at DESC);
