-- name: GetActiveTopics :many
SELECT
    id,
    slug
FROM topics
WHERE active
LIMIT 30;

-- name: ResetVideoTopics :exec
DELETE FROM video_topics
WHERE video_id = sqlc.arg('video_id');

-- name: SetVideoTopic :exec
INSERT INTO video_topics (video_id, topic_id)
VALUES (sqlc.arg('video_id'), sqlc.arg('topic_id'));

-- name: RecommendVideosByTopics :many
-- Videos matching any of the selected topics, in weighted random order:
-- power(random(), 1/w) DESC with w = number of matched topics makes a video
-- that hits all of the user's topics likelier to surface than one that hits a
-- single topic, while still reshuffling on every request.
SELECT
    v.online_video_id,
    v.title,
    v.channel_title,
    v.duration,
    v.thumbnail_url,
    v.thumbnail_width,
    v.thumbnail_height
FROM video_topics vt
JOIN topics tp ON tp.id = vt.topic_id AND tp.active
JOIN videos v ON v.id = vt.video_id
WHERE
    tp.slug = ANY(sqlc.arg('slugs')::text[])
GROUP BY v.id
ORDER BY power(random(), 1.0 / COUNT(*)) DESC
LIMIT sqlc.arg('page_size');
