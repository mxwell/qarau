-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_kazsearch;
ALTER TABLE sentences
ADD COLUMN fts tsvector
GENERATED ALWAYS AS (
    setweight(to_tsvector('simple', text), 'A') ||
    setweight(to_tsvector('kazakh_cfg', text), 'B')
) STORED;

CREATE INDEX idx_sentences_fts ON sentences USING GIN (fts);

-- +goose Down
ALTER TABLE sentences
    DROP COLUMN fts;
