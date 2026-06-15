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