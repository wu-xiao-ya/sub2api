ALTER TABLE groups ADD COLUMN IF NOT EXISTS upstream_provider varchar(20) NOT NULL DEFAULT '';
ALTER TABLE groups ADD CONSTRAINT groups_upstream_provider_check
CHECK (upstream_provider = '' OR (platform = 'openai' AND upstream_provider IN ('qwen', 'hunyuan', 'mimo')));
