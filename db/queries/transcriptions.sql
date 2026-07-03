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
    confidence
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6
);