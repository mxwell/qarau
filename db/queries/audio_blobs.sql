-- name: CreateAudioBlob :one
INSERT INTO audio_blobs (
    job_id,
    content,
    filename
) VALUES (
    sqlc.arg('job_id'),
    sqlc.arg('content'),
    sqlc.arg('filename')
)
RETURNING job_id;