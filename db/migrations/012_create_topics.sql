-- +goose Up
CREATE TABLE topics (
    id SMALLSERIAL PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE video_topics (
    video_id    BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    topic_id    SMALLINT NOT NULL REFERENCES topics(id) ON DELETE RESTRICT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (video_id, topic_id)
);

CREATE INDEX idx_video_topics_topic ON video_topics (topic_id, video_id);

INSERT INTO topics (slug) VALUES
    ('cars'),
    ('comedy'),
    ('food'),
    ('history'),
    ('kids'),
    ('news'),
    ('podcast'),
    ('science'),
    ('sport'),
    ('travel'),
    ('tv_series'),
    ('vlogs');

-- +goose Down
DROP TABLE video_topics;
DROP TABLE topics;
