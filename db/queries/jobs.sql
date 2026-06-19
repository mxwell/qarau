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