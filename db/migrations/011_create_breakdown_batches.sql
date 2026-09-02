-- +goose Up
-- Work queue for on-demand LLM grammar breakdowns. Not a new `job_type`:
-- `jobs` allows one active job per (video, type), while several batches of
-- one video are legitimately in flight at once.
CREATE TYPE batch_state AS ENUM ('pending', 'running', 'done', 'failed');

CREATE TABLE breakdown_batches (
    id               BIGSERIAL   PRIMARY KEY,

    transcription_id BIGINT      NOT NULL,
    batch_start_seq  INT         NOT NULL,
    target_lang      TEXT        NOT NULL,

    state            batch_state NOT NULL DEFAULT 'pending',

    locked_by        TEXT,
    locked_until     TIMESTAMPTZ,

    last_error       TEXT,

    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at       TIMESTAMPTZ,
    finished_at      TIMESTAMPTZ,

    -- Two clients clicking the same position produce one row, so the second
    -- request joins the in-flight batch instead of paying for a duplicate call
    CONSTRAINT uniq_breakdown_batch UNIQUE (transcription_id, batch_start_seq, target_lang),

    -- Re-transcription drops sentences and takes these rows with them, so a
    -- `done` batch can never outlive the breakdowns it produced
    FOREIGN KEY (transcription_id, batch_start_seq)
        REFERENCES sentences (transcription_id, seq) ON DELETE CASCADE
);

CREATE INDEX idx_breakdown_batches_claimable
    ON breakdown_batches (created_at)
    WHERE state IN ('pending', 'running');

-- +goose Down
DROP INDEX IF EXISTS idx_breakdown_batches_claimable;
DROP TABLE breakdown_batches;
DROP TYPE batch_state;
