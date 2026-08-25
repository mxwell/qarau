-- +goose Up
-- Daily cap on LLM breakdown usage. On-demand generation from unauthenticated
-- clients is otherwise unbounded spend. Counted in tokens rather than calls,
-- since batch sizes vary and tokens are what's billed. Input and output are
-- kept apart because they're priced differently.
-- Day boundary is KZ time (see internal/quota), mirroring asr_quota.
CREATE TABLE llm_quota (
    day                 date   PRIMARY KEY,
    used_input_tokens   BIGINT NOT NULL DEFAULT 0,
    used_output_tokens  BIGINT NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE llm_quota;
