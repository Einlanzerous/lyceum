-- LYCM-801: user accounts, session/invite tokens, and per-user reading positions.

CREATE TABLE IF NOT EXISTS users (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email        TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    is_owner     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- At most one owner. The partial index only covers is_owner = true rows, so
-- ordinary members are unconstrained.
CREATE UNIQUE INDEX IF NOT EXISTS users_single_owner ON users ((is_owner)) WHERE is_owner;

-- Credentials for a user. An 'invite' is single-use (the owner hands it out) and
-- is redeemed for a long-lived 'session', so a device can be revoked on its own
-- and a person can be re-invited without being deleted. Only the SHA-256 of the
-- token is stored; the plaintext is shown once at mint time and never again.
CREATE TABLE IF NOT EXISTS user_tokens (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('invite', 'session')),
    token_hash   TEXT NOT NULL UNIQUE,
    label        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    used_at      TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS user_tokens_user_id ON user_tokens (user_id);

-- Seed the owner here rather than at boot, so the reading_positions backfill
-- below has a user to point at and the whole migration lands in one transaction.
-- Email and display name are placeholders that cmd/lyceum reconciles from
-- LYCEUM_OWNER_EMAIL / LYCEUM_OWNER_NAME on startup.
INSERT INTO users (email, display_name, is_owner)
SELECT 'owner@localhost', 'Reader', TRUE
WHERE NOT EXISTS (SELECT 1 FROM users);

-- Reading positions become per-user. Everything recorded before accounts existed
-- was read by the single pre-accounts user, who is now the owner.
ALTER TABLE reading_positions
    ADD COLUMN IF NOT EXISTS user_id BIGINT REFERENCES users(id) ON DELETE CASCADE;

UPDATE reading_positions
   SET user_id = (SELECT id FROM users WHERE is_owner)
 WHERE user_id IS NULL;

ALTER TABLE reading_positions ALTER COLUMN user_id SET NOT NULL;

-- The identity of a bookmark is now (book, user, device), not (book, device):
-- two people reading the same book on the same shared device each own a row.
ALTER TABLE reading_positions
    DROP CONSTRAINT IF EXISTS reading_positions_book_id_device_id_key;

ALTER TABLE reading_positions
    ADD CONSTRAINT reading_positions_book_user_device_key
    UNIQUE (book_id, user_id, device_id);
