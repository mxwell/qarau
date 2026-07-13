-- name: UpsertTranscription :one
INSERT INTO transcriptions (
    video_id,
    model
) VALUES (
    sqlc.arg('video_id'),
    sqlc.arg('model')
)
ON CONFLICT (video_id, model) DO UPDATE
    SET created_at = NOW()
RETURNING id;

-- name: DeleteWordsByTranscriptionId :exec
DELETE FROM words WHERE transcription_id = sqlc.arg('transcription_id');

-- name: InsertWords :copyfrom
INSERT INTO words (
    transcription_id,
    seq,
    start_ms,
    end_ms,
    word,
    confidence,
    speaker
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7
);

-- name: GetTranscription :one
SELECT * FROM transcriptions
WHERE id = sqlc.arg('id')
LIMIT 1;

-- name: GetTranscriptionsByVideoID :many
SELECT * FROM transcriptions
WHERE video_id = sqlc.arg('video_id');

-- name: GetWords :many
SELECT * FROM words
WHERE
    transcription_id = sqlc.arg('transcription_id') AND
    seq >= sqlc.arg('start_seq')
ORDER BY seq ASC
LIMIT sqlc.arg('word_count');

-- name: FindSeqByStartMs :one
SELECT seq FROM words
WHERE
    transcription_id = sqlc.arg('transcription_id') AND
    start_ms >= sqlc.arg('start_ms')
ORDER BY seq
LIMIT 1;