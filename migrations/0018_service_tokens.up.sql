-- Service tokens (LYCM-140): scoped bearer credentials for other programs
-- (Eidolon, the Kindle sender, Satlink), replacing the old static env token
-- table. Only the SHA-256 of a token is stored; the plaintext is shown once, by
-- the CLI that mints it. Revoking sets revoked_at and keeps the row.
--
-- consumer_label is deliberately not unique: rotation holds two live tokens for
-- one consumer until the old one is revoked. One scope per token.
CREATE TABLE IF NOT EXISTS service_tokens (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    consumer_label TEXT NOT NULL,
    scope          TEXT NOT NULL,
    token_hash     TEXT NOT NULL UNIQUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at   TIMESTAMPTZ,
    revoked_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS service_tokens_consumer_label ON service_tokens (consumer_label);

-- An append-only trail of credential events. No foreign keys, so a row outlives
-- the thing it describes (a deleted user, a token that is long gone).
CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor       TEXT NOT NULL,
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id   TEXT NOT NULL,
    detail      JSONB NOT NULL DEFAULT '{}'::jsonb
);

-- The refused-token dedup asks "any row for this prefix in the last hour".
CREATE INDEX IF NOT EXISTS audit_log_action_target_at ON audit_log (action, target_id, at);
