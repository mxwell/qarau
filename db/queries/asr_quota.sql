-- name: GetAsrQuota :one
SELECT
    used_seconds
FROM asr_quota
WHERE day = sqlc.arg('day');

-- name: UpsertAsrQuota :one
INSERT INTO asr_quota (
    day,
    used_seconds
) VALUES (
    sqlc.arg('day'),
    sqlc.arg('used_seconds')
)
ON CONFLICT (day) DO UPDATE
SET used_seconds = asr_quota.used_seconds + EXCLUDED.used_seconds
RETURNING used_seconds;
