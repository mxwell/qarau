-- +goose Up
CREATE INDEX idx_jobs_created_at
    ON jobs (created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_jobs_created_at;