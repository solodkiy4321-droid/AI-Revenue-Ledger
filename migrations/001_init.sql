CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_users_role CHECK (role IN ('owner', 'analyst', 'viewer')),
    CONSTRAINT uq_users_tenant_email UNIQUE (tenant_id, email)
);

CREATE TABLE integrations (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    status TEXT NOT NULL,
    last_sync_at TIMESTAMPTZ,
    error_message TEXT,
    CONSTRAINT chk_integrations_status CHECK (status IN ('connected','error','disconnected')),
    CONSTRAINT chk_integrations_provider CHECK (provider IN ('ga4', 'gsc', 'hubspot')),
    CONSTRAINT uq_integrations_tenant_provider UNIQUE (tenant_id, provider)
);

CREATE TABLE reports (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    confirmed_amount NUMERIC(15, 2) NOT NULL,
    declared_amount NUMERIC(15, 2) NOT NULL,
    modeled_low NUMERIC(15, 2) NOT NULL,
    modeled_high NUMERIC(15, 2) NOT NULL,
    methodology_version TEXT NOT NULL,
    calculated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    stale BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX idx_users_tenant ON users(tenant_id);
CREATE INDEX idx_integrations_tenant ON integrations(tenant_id);
CREATE INDEX idx_reports_tenant ON reports(tenant_id);