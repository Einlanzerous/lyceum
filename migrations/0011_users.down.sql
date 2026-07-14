-- Revert LYCM-801. Positions belonging to non-owner users are dropped: without
-- users there is nowhere to put them, and collapsing them onto the owner would
-- silently overwrite the owner's own bookmarks.

DELETE FROM reading_positions
 WHERE user_id NOT IN (SELECT id FROM users WHERE is_owner);

ALTER TABLE reading_positions
    DROP CONSTRAINT IF EXISTS reading_positions_book_user_device_key;

ALTER TABLE reading_positions DROP COLUMN IF EXISTS user_id;

ALTER TABLE reading_positions
    ADD CONSTRAINT reading_positions_book_id_device_id_key
    UNIQUE (book_id, device_id);

DROP TABLE IF EXISTS user_tokens;
DROP TABLE IF EXISTS users;
