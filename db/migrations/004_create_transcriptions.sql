-- +goose Up
CREATE TABLE transcriptions (
    id BIGSERIAL PRIMARY KEY,
    video_id BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    model TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uniq_video_model UNIQUE (video_id, model)
);

CREATE TABLE words (
    transcription_id BIGINT NOT NULL REFERENCES transcriptions(id) ON DELETE CASCADE,
    seq INT NOT NULL,

    start_ms INT NOT NULL,
    end_ms INT NOT NULL,

    word TEXT NOT NULL,
    confidence SMALLINT NOT NULL,  -- per cent

    CHECK (confidence BETWEEN 0 AND 100),
    PRIMARY KEY (transcription_id, seq)
);

-- +goose Down
DROP TABLE words;
DROP TABLE transcriptions;