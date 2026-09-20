-- name: InsertSentences :copyfrom
INSERT INTO sentences (
    transcription_id,
    seq,
    start_word_seq,
    end_word_seq,
    start_ms,
    end_ms,
    text
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7
);

-- name: DeleteSentencesByTranscriptionId :exec
DELETE FROM sentences WHERE transcription_id = sqlc.arg('transcription_id');

-- name: FindSentenceSeqByStartMs :one
-- The sentence being spoken at start_ms: the last one that has already begun.
-- Returns no rows when start_ms precedes the first sentence — callers fall back
-- to GetFirstSentenceSeq.
SELECT seq FROM sentences
WHERE
    transcription_id = sqlc.arg('transcription_id') AND
    start_ms <= sqlc.arg('start_ms')
ORDER BY start_ms DESC
LIMIT 1;

-- name: GetFirstSentenceSeq :one
SELECT seq FROM sentences
WHERE transcription_id = sqlc.arg('transcription_id')
ORDER BY seq ASC
LIMIT 1;

-- name: GetSentencesRange :many
SELECT
    seq,
    start_word_seq,
    end_word_seq,
    start_ms,
    end_ms,
    text
FROM sentences
WHERE
    transcription_id = sqlc.arg('transcription_id') AND
    seq BETWEEN sqlc.arg('start_seq') AND sqlc.arg('end_seq')
ORDER BY seq ASC;

-- name: GetSentenceBreakdowns :many
-- Whatever breakdown exists for these sentences, regardless of which model or
-- prompt produced it.
SELECT
    sentence_seq,
    translations,
    breakdown
FROM sentence_breakdowns
WHERE
    transcription_id = sqlc.arg('transcription_id') AND
    sentence_seq BETWEEN sqlc.arg('start_seq') AND sqlc.arg('end_seq') AND
    target_lang = sqlc.arg('target_lang')
ORDER BY sentence_seq ASC;

-- name: CountSentenceBreakdowns :one
SELECT
    COUNT(*)
FROM sentence_breakdowns
WHERE
    transcription_id = sqlc.arg('transcription_id') AND
    sentence_seq BETWEEN sqlc.arg('start_seq') AND sqlc.arg('end_seq') AND
    target_lang = sqlc.arg('target_lang');

-- name: InsertSentenceBreakdown :exec
-- Only ever called for sentences that have no breakdown yet, so a conflict
-- means two clients clicked the same position concurrently. Keep the first
-- one: the loser of the race must not error, and the rows are equivalent.
--
-- XXX `InsertSentenceBreakdowns :batchexec` might be a better fit
-- when dozens of rows are inserted at once.
INSERT INTO sentence_breakdowns (
    transcription_id,
    sentence_seq,
    target_lang,
    model,
    prompt_version,
    translations,
    breakdown
) VALUES (
    sqlc.arg('transcription_id'),
    sqlc.arg('sentence_seq'),
    sqlc.arg('target_lang'),
    sqlc.arg('model'),
    sqlc.arg('prompt_version'),
    sqlc.arg('translations'),
    sqlc.arg('breakdown')
)
ON CONFLICT (transcription_id, sentence_seq, target_lang)
DO NOTHING;

-- name: FindSentenceFts :many
WITH q AS (
    SELECT websearch_to_tsquery('simple',     sqlc.arg('query')) AS exact_q,
           websearch_to_tsquery('kazakh_cfg', sqlc.arg('query')) AS stem_q
),
hits AS (
    SELECT s.transcription_id, s.seq, s.start_ms, s.end_ms, s.text,
           ts_rank(s.fts, q.exact_q || q.stem_q) AS rank
    FROM sentences s
    CROSS JOIN q
    WHERE s.fts @@ (q.exact_q || q.stem_q)
    ORDER BY rank DESC, s.transcription_id, s.seq
    LIMIT sqlc.arg('limit')
)
SELECT
    h.transcription_id,
    t.video_id,
    v.online_video_id,
    v.title,
    v.channel_title,
    h.seq, h.start_ms, h.end_ms, h.text, h.rank,
    ts_headline('kazakh_cfg', h.text, q.exact_q || q.stem_q,
        'StartSel=<b>, StopSel=</b>, HighlightAll=true') AS hl
FROM hits h
CROSS JOIN q
JOIN transcriptions t ON t.id = h.transcription_id
JOIN videos v         ON v.id = t.video_id
ORDER BY h.rank DESC, h.transcription_id, h.seq;