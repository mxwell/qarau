-- name: EnqueueBreakdownBatch :one
-- Insert, or join the existing batch. A `failed` batch goes back to `pending`,
-- so asking again after a failure is what retries it. Returns the row either
-- way; the caller reads `state` to decide what to tell the client.
INSERT INTO breakdown_batches (
    transcription_id,
    batch_start_seq,
    target_lang
) VALUES (
    sqlc.arg('transcription_id'),
    sqlc.arg('batch_start_seq'),
    sqlc.arg('target_lang')
)
ON CONFLICT (transcription_id, batch_start_seq, target_lang) DO UPDATE
SET
    state = CASE
        WHEN breakdown_batches.state = 'failed' THEN 'pending'
        ELSE breakdown_batches.state
    END,
    last_error = CASE
        WHEN breakdown_batches.state = 'failed' THEN NULL
        ELSE breakdown_batches.last_error
    END
RETURNING id, state, last_error;

-- name: GetBreakdownBatch :one
SELECT
    id,
    state,
    last_error,
    created_at,
    finished_at
FROM breakdown_batches
WHERE
    transcription_id = sqlc.arg('transcription_id') AND
    batch_start_seq = sqlc.arg('batch_start_seq') AND
    target_lang = sqlc.arg('target_lang');

-- name: CountActiveBreakdownBatches :one
-- Bound on unfinished work per transcription: a viewer clicking around must
-- not queue dozens of paid LLM calls.
SELECT count(*) FROM breakdown_batches
WHERE
    transcription_id = sqlc.arg('transcription_id') AND
    state IN ('pending', 'running');

-- name: ClaimBreakdownBatch :one
-- Picking up an expired `running` lease is crash recovery: every in-process
-- error path marks the batch `failed` itself.
UPDATE breakdown_batches SET
    state = 'running',
    locked_by = sqlc.arg('locked_by'),
    locked_until = sqlc.arg('locked_until'),
    started_at = now()
WHERE id = (
    SELECT nested_batches.id FROM breakdown_batches AS nested_batches
    WHERE
        nested_batches.state = 'pending' OR
        (nested_batches.state = 'running' AND nested_batches.locked_until < now())
    ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED
)
RETURNING id, transcription_id, batch_start_seq, target_lang;

-- name: MarkBreakdownBatchDone :one
UPDATE breakdown_batches SET
    state = 'done',
    locked_by = NULL,
    locked_until = NULL,
    last_error = NULL,
    finished_at = now()
WHERE
    id = sqlc.arg('batch_id') AND
    state = 'running' AND
    locked_by = sqlc.arg('locked_by')
RETURNING id;

-- name: MarkBreakdownBatchFailed :one
-- Covers both a broken batch and a blocked one (daily LLM quota used up):
-- either way it stops being claimable until the user asks for it again.
UPDATE breakdown_batches SET
    state = 'failed',
    locked_by = NULL,
    locked_until = NULL,
    last_error = sqlc.arg('error_message'),
    finished_at = now()
WHERE
    id = sqlc.arg('batch_id') AND
    state = 'running' AND
    locked_by = sqlc.arg('locked_by')
RETURNING id;

-- name: GetLastBreakdownBatches :many
SELECT
    bt.id,
    bt.state,
    bt.target_lang,
    bt.last_error,
    bt.created_at,
    vt.online_video_id,
    vt.title
FROM breakdown_batches AS bt
JOIN transcriptions AS tt ON bt.transcription_id = tt.id
JOIN videos AS vt ON tt.video_id = vt.id
ORDER BY bt.created_at DESC
LIMIT sqlc.arg('batches');
