CREATE TABLE IF NOT EXISTS integrations (
    id BIGSERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('ga4', 'gsc', 'hubspot')),
    status TEXT NOT NULL CHECK (status IN ('connected', 'disconnected', 'error')),
    last_sync_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    stale BOOLEAN NOT NULL DEFAULT FALSE,
    account_label TEXT,
    last_error TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_integrations_tenant_provider UNIQUE (tenant_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_integrations_tenant_id ON integrations (tenant_id);

CREATE TABLE IF NOT EXISTS jobs (
    job_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    job_type TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed')),
    progress DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    estimated_finish_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_jobs_tenant_id ON jobs (tenant_id);
CREATE INDEX IF NOT EXISTS idx_jobs_tenant_type_status ON jobs (tenant_id, job_type, status);

CREATE TABLE IF NOT EXISTS report_summaries (
    report_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    period TEXT NOT NULL CHECK (period IN ('30d', '90d', '180d')),
    methodology_version TEXT NOT NULL,
    calibration_date DATE NOT NULL,
    stale BOOLEAN NOT NULL DEFAULT FALSE,
    currency TEXT NOT NULL DEFAULT 'USD',
    confirmed_amount INTEGER NOT NULL,
    confirmed_deals INTEGER NOT NULL,
    declared_amount INTEGER NOT NULL,
    declared_deals INTEGER NOT NULL,
    declared_coverage_note TEXT NOT NULL,
    modeled_amount_low INTEGER NOT NULL,
    modeled_amount_high INTEGER NOT NULL,
    modeled_error_margin TEXT NOT NULL,
    disclaimer TEXT NOT NULL,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_report_summaries_tenant_period
    ON report_summaries (tenant_id, period, generated_at DESC);