-- Keep deployed WorkBuddy installations, encrypted config and account IDs intact.
WITH created AS (
    INSERT INTO sub2api_plugin_installations (
        plugin_key, name, version, manifest, artifact_path, install_path,
        binary_path, binary_sha256, state, enabled_at
    ) VALUES (
        'org.starlight.workbuddy', 'WorkBuddy 国内版 DeepSeek', '0.1.3',
        '{
            "schema_version": 1,
            "id": "org.starlight.workbuddy",
            "name": "WorkBuddy 国内版 DeepSeek",
            "version": "0.1.3",
            "requires": {"sub2api": ">=0.2.18 <0.3.0", "plugin_protocol": 1, "transport_api": 1, "ui_bridge": 1},
            "capabilities": [{"id": "upstream.chat_completions.v1", "platform": "deepseek", "account_type": "plugin"}],
            "authorization_hosts": ["www.codebuddy.cn"],
            "ui": {"entrypoint": "ui/index.html"}
        }'::jsonb,
        '', '', '', repeat('0', 64), 'enabled', NOW()
    )
    ON CONFLICT (plugin_key) DO NOTHING
    RETURNING id
)
INSERT INTO sub2api_plugin_bindings (plugin_id, capability, platform, account_type, enabled)
SELECT id, 'upstream.chat_completions.v1', 'deepseek', 'plugin', TRUE FROM created;
