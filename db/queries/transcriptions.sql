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

-- name: GetSuggestedVideos :many
  SELECT
      v.id,
      v.online_video_id,
      v.title,
      v.channel_id,
      v.channel_title,
      v.published_at,
      v.duration,
      v.views,
      v.likes,
      v.default_lang,
      v.embeddable,
      v.thumbnail_url,
      v.thumbnail_width,
      v.thumbnail_height
  FROM transcriptions t
  JOIN videos v ON v.id = t.video_id
  ORDER BY random()
  LIMIT 10;