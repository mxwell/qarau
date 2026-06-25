-- name: GetJob :one
SELECT
    id,
    state,
    type,
    locked_by,
    attempts,
    max_attempts
FROM jobs
WHERE id = sqlc.arg('job_id');

-- name: GetVideoJobs :many
SELECT
    id,
    state,
    type
FROM jobs
WHERE
    video_id = sqlc.arg('video_id');

-- name: CreateFetchJobIfAbsent :one
INSERT INTO jobs (
    video_id,
    type,
    online_video_id
) VALUES (
    sqlc.arg('video_id'),
    'fetch',
    sqlc.arg('online_video_id')
)
ON CONFLICT (video_id, type) DO NOTHING
RETURNING id;

-- name: ClaimJob :one
UPDATE jobs SET
    state = 'running',
    locked_by = sqlc.arg('locked_by'),
    locked_until = sqlc.arg('locked_until'),
    attempts = attempts + 1,
    started_at = COALESCE(started_at, now())
WHERE id = (
    SELECT nested_jobs.id FROM jobs AS nested_jobs
    WHERE
        nested_jobs.type = sqlc.arg('job_type') AND
        nested_jobs.attempts < nested_jobs.max_attempts AND
        (
            nested_jobs.state = 'pending' OR
            (nested_jobs.state = 'running' AND nested_jobs.locked_until < now())
        )
    ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED
)
RETURNING id, video_id, online_video_id;

-- name: MarkJobDone :one
UPDATE jobs SET
    state = 'done',
    last_error = NULL,
    finished_at = now()
WHERE
    id = sqlc.arg('job_id') AND
    type = sqlc.arg('job_type') AND
    state = 'running' AND
    locked_by = sqlc.arg('locked_by')
RETURNING id;

-- name: CreateAsrJob :one
INSERT INTO jobs (
    video_id,
    type,
    online_video_id
) SELECT video_id, 'asr', online_video_id
FROM jobs AS nested_jobs
WHERE nested_jobs.id = sqlc.arg('fetch_job_id') AND nested_jobs.type = 'fetch'
RETURNING id;

-- name: UnlockJob :one
UPDATE jobs SET
    state = 'pending',
    locked_by = NULL,
    locked_until = NULL,
    last_error = sqlc.arg('error_message')
WHERE
    id = sqlc.arg('job_id') AND
    state = 'running' AND
    locked_by = sqlc.arg('locked_by')
RETURNING id;

-- name: MarkJobFailed :one
UPDATE jobs SET
    state = 'failed',
    locked_by = NULL,
    locked_until = NULL,
    last_error = sqlc.arg('error_message')
WHERE
    id = sqlc.arg('job_id') AND
    state = 'running' AND
    locked_by = sqlc.arg('locked_by')
RETURNING id;