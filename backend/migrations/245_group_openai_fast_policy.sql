-- Group-level OpenAI Fast policy, ported from upstream Sub2API 0.2.0.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS force_openai_fast BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS free_openai_fast BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE groups
SET force_openai_fast = FALSE,
    free_openai_fast = FALSE
WHERE platform NOT IN ('openai', 'composite');
