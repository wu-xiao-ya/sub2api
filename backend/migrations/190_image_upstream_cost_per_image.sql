-- Configurable upstream cost for image cost/profit reporting.
-- This is intentionally separate from user-facing image pricing.
INSERT INTO settings (key, value, updated_at)
VALUES ('image_upstream_cost_per_image', '0.001', NOW())
ON CONFLICT (key) DO NOTHING;
