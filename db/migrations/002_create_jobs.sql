-- +goose Up
-- Enumerations
CREATE TYPE job_type AS ENUM ('fetch', 'asr');
CREATE TYPE job_state AS ENUM ('pending', 'running', 'done', 'failed');

-- Jobs table: work queue for fetch and asr tasks
CREATE TABLE jobs (
    -- Identity
    id              BIGSERIAL PRIMARY KEY,
    video_id        BIGINT      NOT NULL REFERENCES videos(id),
    type            job_type    NOT NULL,
    state           job_state   NOT NULL DEFAULT 'pending',

    -- Leasing / claim fields
    locked_by       TEXT,                   -- worker instance id
    locked_until    TIMESTAMPTZ,
    attempts        INT         NOT NULL DEFAULT 0,
    max_attempts    INT         NOT NULL DEFAULT 3,

    -- Progress & error tracking
    progress_percent SMALLINT   NOT NULL DEFAULT 0,
    last_error      TEXT,

    -- Payload (input for worker)
    online_video_id TEXT        NOT NULL,

    -- Timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,

    -- Constraints
    CONSTRAINT uniq_active_job UNIQUE (video_id, type)  -- one active job per (video, type), second chance is given to failed jobs by manual intervention
);

-- Index for claim query: find oldest claimable job of a given type
CREATE INDEX idx_jobs_claimable
    ON jobs (type, created_at)
    WHERE state IN ('pending', 'running');

-- +goose Down
DROP INDEX IF EXISTS idx_jobs_claimable;
DROP TABLE jobs;
DROP TYPE job_state;
DROP TYPE job_type;