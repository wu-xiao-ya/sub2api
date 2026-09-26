-- GPT intelligence degradation probe (pelican SVG test).
-- Targets are group+model pairs probed periodically through the internal
-- gateway; every run stores the returned SVG so an operator can eyeball
-- whether the group serves degraded models.

CREATE TABLE IF NOT EXISTS intelligence_probe_targets (
    id BIGSERIAL PRIMARY KEY,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    model VARCHAR(200) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_intelligence_probe_targets_due
    ON intelligence_probe_targets (next_run_at) WHERE enabled = TRUE;

CREATE TABLE IF NOT EXISTS intelligence_probe_results (
    id BIGSERIAL PRIMARY KEY,
    target_id BIGINT NOT NULL REFERENCES intelligence_probe_targets(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL,
    response_svg TEXT NOT NULL DEFAULT '',
    response_excerpt TEXT NOT NULL DEFAULT '',
    error_message VARCHAR(500) NOT NULL DEFAULT '',
    latency_ms INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT intelligence_probe_results_status_check
        CHECK (status IN ('success', 'no_svg', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_intelligence_probe_results_target_created
    ON intelligence_probe_results (target_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_intelligence_probe_results_created
    ON intelligence_probe_results (created_at);
