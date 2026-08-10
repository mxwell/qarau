-- +goose Up
-- Playlists table: suggested playlists in UI
-- Initially created with online_playlist_id
-- On the first load, details are loaded from API and saved
-- If API load failed, the playlist is marked with non-null error
CREATE TABLE playlists (
    id                  BIGSERIAL   PRIMARY KEY,
    online_playlist_id  TEXT        NOT NULL UNIQUE,

    error               TEXT,
    title               TEXT NOT NULL DEFAULT '',
    item_count          INT NOT NULL DEFAULT 0,

    -- Thumbnail (preferred size)
    thumbnail_url       TEXT NOT NULL DEFAULT '',
    thumbnail_width     INT NOT NULL DEFAULT 0,
    thumbnail_height    INT NOT NULL DEFAULT 0,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE playlists;
