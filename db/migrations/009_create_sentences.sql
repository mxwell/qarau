-- +goose Up
-- Sentences: word ranges of a transcription's word stream, split on
-- sentence-final punctuation. Derived from `words`, so no `model` column:
-- segmentation depends only on the word stream, not on who annotates it.
CREATE TABLE sentences (
    transcription_id BIGINT NOT NULL REFERENCES transcriptions(id) ON DELETE CASCADE,
    seq              INT    NOT NULL,

    -- Range into words(seq), both ends inclusive
    start_word_seq   INT    NOT NULL,
    end_word_seq     INT    NOT NULL,

    -- Denormalized from the first/last word of the range: playback position ->
    -- sentence must not need a join, and the client follows playback by these
    start_ms         INT    NOT NULL,
    end_ms           INT    NOT NULL,

    text             TEXT   NOT NULL,

    CHECK (end_word_seq >= start_word_seq),
    CHECK (end_ms >= start_ms),
    PRIMARY KEY (transcription_id, seq)
);

-- Hot path: playback position T -> sentence, on every breakdown click
CREATE INDEX idx_sentences_start_ms ON sentences (transcription_id, start_ms);

-- Grammar breakdown + translation produced by an LLM, cached per sentence.
-- Batching (25 sentences per LLM call) is a request-time concept only, so
-- changing the batch size never invalidates this cache.
CREATE TABLE sentence_breakdowns (
    transcription_id BIGINT NOT NULL,
    sentence_seq     INT    NOT NULL,

    -- Cache identity. Only the target language distinguishes rows: coverage
    -- beats freshness here, so an existing breakdown is served whatever
    -- produced it, and one sentence never holds competing versions.
    target_lang      TEXT   NOT NULL,

    -- Provenance, not identity. Never read by the serving path; recorded so a
    -- future regeneration pass can tell what produced each row.
    model            TEXT   NOT NULL,
    prompt_version   INT    NOT NULL,

    translations     JSONB  NOT NULL,  -- array of translation variants
    breakdown        JSONB  NOT NULL,  -- array of per-word/phrase objects

    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (transcription_id, sentence_seq, target_lang),

    -- Cache invalidation: dropping a transcription's sentences on
    -- re-transcription drops their breakdowns with them
    FOREIGN KEY (transcription_id, sentence_seq)
        REFERENCES sentences (transcription_id, seq) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE sentence_breakdowns;
DROP INDEX IF EXISTS idx_sentences_start_ms;
DROP TABLE sentences;
