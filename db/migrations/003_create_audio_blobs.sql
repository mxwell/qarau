-- +goose Up
CREATE TABLE audio_blobs (
    video_id BIGINT PRIMARY KEY REFERENCES videos(id) ON DELETE CASCADE,

    content BYTEA NOT NULL,
    filename TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE audio_blobs;