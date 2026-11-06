ALTER TABLE groups ADD COLUMN IF NOT EXISTS upstream_provider varchar(20) NOT NULL DEFAULT '';
ALTER TABLE groups ADD CONSTRAINT groups_upstream_provider_check
CHECK (upstream_provider = '' OR (platform = 'openai' AND upstream_provider IN ('qwen', 'hunyuan', 'mimo')));

-- Brand changes must invalidate cached group DTOs even for offline backfills.
CREATE FUNCTION enqueue_group_brand_auth_cache_invalidation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO auth_cache_invalidation_outbox(cache_key)
 SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex') FROM api_keys k
 WHERE k.group_id=NEW.id AND k.deleted_at IS NULL AND k.key<>'';
 RETURN NEW;
END;
$$;
CREATE TRIGGER trg_group_brand_auth_cache_invalidation AFTER UPDATE OF upstream_provider ON groups
FOR EACH ROW WHEN (OLD.upstream_provider IS DISTINCT FROM NEW.upstream_provider)
EXECUTE FUNCTION enqueue_group_brand_auth_cache_invalidation();
