-- name: GetLlmQuota :one
SELECT
    used_input_tokens,
    used_output_tokens
FROM llm_quota
WHERE day = sqlc.arg('day');

-- name: UpsertLlmQuota :one
INSERT INTO llm_quota (
    day,
    used_input_tokens,
    used_output_tokens
) VALUES (
    sqlc.arg('day'),
    sqlc.arg('used_input_tokens'),
    sqlc.arg('used_output_tokens')
)
ON CONFLICT (day) DO UPDATE
SET
    used_input_tokens = llm_quota.used_input_tokens + EXCLUDED.used_input_tokens,
    used_output_tokens = llm_quota.used_output_tokens + EXCLUDED.used_output_tokens
RETURNING used_input_tokens, used_output_tokens;
