-- +goose Up
CREATE TABLE asr_quota (
    day date PRIMARY KEY,
    used_seconds INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE asr_quota;