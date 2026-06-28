-- LYCM-102: initial Lyceum schema.

CREATE TABLE IF NOT EXISTS books (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title      TEXT NOT NULL,
    author     TEXT,
    cover_path TEXT,
    file_path  TEXT NOT NULL,
    file_hash  TEXT NOT NULL UNIQUE,
    size_bytes BIGINT,
    added_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS devices (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name      TEXT,
    last_seen TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS reading_positions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    book_id    BIGINT REFERENCES books(id) ON DELETE CASCADE,
    device_id  TEXT NOT NULL,
    cfi        TEXT NOT NULL,
    progress   DOUBLE PRECISION,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (book_id, device_id)
);
