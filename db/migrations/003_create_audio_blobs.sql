-- +goose Up
CREATE TABLE audio_blobs (
    job_id BIGINT PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,

    content BYTEA NOT NULL,
    filename TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE audio_blobs;