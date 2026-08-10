-- name: ListPlaylists :many
SELECT
    id,
    online_playlist_id,
    error,
    title,
    item_count,
    thumbnail_url,
    thumbnail_width,
    thumbnail_height,
    created_at
FROM playlists
WHERE
    error IS NULL AND
    (sqlc.narg('cursor')::bigint IS NULL OR id < sqlc.narg('cursor'))
ORDER BY id DESC
LIMIT sqlc.arg('page_size');

-- name: UpdatePlaylistDetails :exec
UPDATE playlists
SET
    error = NULL,
    title = sqlc.arg('title'),
    item_count = sqlc.arg('item_count'),
    thumbnail_url = sqlc.arg('thumbnail_url'),
    thumbnail_width = sqlc.arg('thumbnail_width'),
    thumbnail_height = sqlc.arg('thumbnail_height')
WHERE
    id = sqlc.arg('id');

-- name: UpdatePlaylistWithError :exec
UPDATE playlists
SET
    error = sqlc.arg('error')
WHERE
    id = sqlc.arg('id');
