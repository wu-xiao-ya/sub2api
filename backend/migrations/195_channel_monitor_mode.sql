-- Channel monitor preferred user-facing view: v1 (active diagnostics) or v2
-- (passive real-request trends). The hybrid runtime keeps both monitor layers
-- active while this setting selects the default user-facing view.
INSERT INTO settings (key, value)
VALUES ('channel_monitor_mode', 'v2')
ON CONFLICT (key) DO NOTHING;
