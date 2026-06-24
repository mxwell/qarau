-- name: GetVideo :one
SELECT
    id,
    online_video_id,
    title,
    channel_id,
    channel_title,
    published_at,
    duration,
    default_lang,
    embeddable,
    thumbnail_url,
    thumbnail_width,
    thumbnail_height
FROM videos
WHERE online_video_id = sqlc.arg('online_video_id')
LIMIT 1;

-- name: GetVideoByID :one
SELECT
    id,
    online_video_id,
    title,
    channel_id,
    channel_title,
    published_at,
    duration,
    default_lang,
    embeddable,
    thumbnail_url,
    thumbnail_width,
    thumbnail_height
FROM videos
WHERE id = sqlc.arg('video_id')
LIMIT 1;

-- name: CreateVideo :one
INSERT INTO videos (
    online_video_id,
    title,
    channel_id,
    channel_title,
    published_at,
    duration,
    default_lang,
    embeddable,
    thumbnail_url,
    thumbnail_width,
    thumbnail_height
) VALUES (
    sqlc.arg('online_video_id'),
    sqlc.arg('title'),
    sqlc.arg('channel_id'),
    sqlc.arg('channel_title'),
    sqlc.arg('published_at'),
    sqlc.arg('duration'),
    sqlc.arg('default_lang'),
    sqlc.arg('embeddable'),
    sqlc.arg('thumbnail_url'),
    sqlc.arg('thumbnail_width'),
    sqlc.arg('thumbnail_height')
)
RETURNING id;