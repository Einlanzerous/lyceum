-- LYCM-88: short, human-typeable pairing codes as an alternative carrier for a
-- single-use invite. Each row stands for exactly one invite (a user_tokens row);
-- redeeming the code redeems that invite. Codes are hashed at rest like the
-- tokens they front, but they are far lower-entropy than the 256-bit invite, so
-- they must be short-lived (see store.PairingTTL) and the exchange endpoint is
-- rate-limited. ON DELETE CASCADE ties a code's lifetime to its invite: reissue
-- or delete the invite and the code goes with it.
CREATE TABLE IF NOT EXISTS pairing_codes (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token_id   BIGINT NOT NULL REFERENCES user_tokens(id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS pairing_codes_token_id ON pairing_codes (token_id);
