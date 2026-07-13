-- +goose Up
-- Videos table: metadata for source videos
CREATE TABLE videos (
    -- Identity
    id                BIGSERIAL   PRIMARY KEY,
    online_video_id   TEXT        NOT NULL UNIQUE,

    -- Core metadata
    title             TEXT        NOT NULL,
    channel_id        TEXT        NOT NULL,          -- Channel UCID
    channel_title     TEXT        NOT NULL,          -- Channel display name
    published_at      TIMESTAMPTZ NOT NULL,          -- When video was published
    duration          INTERVAL    NOT NULL,           -- Video length (e.g., "4 mins 13 secs")

    -- Stats
    views             BIGINT NOT NULL,
    likes             BIGINT NOT NULL,

    -- Additional metadata
    default_lang      TEXT,                          -- Audio language code (e.g., "en", "es")
    embeddable        BOOLEAN     NOT NULL,          -- Can be embedded on 3rd party sites

    -- Thumbnail (preferred size)
    thumbnail_url     TEXT,
    thumbnail_width   INT,
    thumbnail_height  INT,

    -- Timestamps
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE videos;