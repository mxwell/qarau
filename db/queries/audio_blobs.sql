-- name: CreateAudioBlob :one
INSERT INTO audio_blobs (
    video_id,
    content,
    filename
) VALUES (
    sqlc.arg('video_id'),
    sqlc.arg('content'),
    sqlc.arg('filename')
)
RETURNING video_id;

-- name: GetAudioBlob :one
SELECT
    content,
    filename
FROM audio_blobs
WHERE video_id = sqlc.arg('video_id');