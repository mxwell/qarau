-- +goose Up
ALTER TABLE words
    ADD COLUMN speaker INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE words
    DROP COLUMN speaker;