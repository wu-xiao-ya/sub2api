-- Explicit per-account bindings; credentials remain in encrypted plugin config.
CREATE TABLE sub2api_plugin_account_bindings (
 account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 plugin_id BIGINT NOT NULL REFERENCES sub2api_plugin_installations(id) ON DELETE RESTRICT,
 identity_ref VARCHAR(160) NOT NULL CHECK (length(identity_ref)>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(plugin_id, identity_ref)
);
-- Account bindings, rather than a global provider slot, select provider plugins.
DROP INDEX idx_sub2api_plugin_bindings_one_enabled_scope;
CREATE UNIQUE INDEX idx_sub2api_plugin_bindings_one_enabled_scope
 ON sub2api_plugin_bindings(capability,platform,account_type)
 WHERE enabled=TRUE AND capability='openai.oauth.outbound_transport.v1';
