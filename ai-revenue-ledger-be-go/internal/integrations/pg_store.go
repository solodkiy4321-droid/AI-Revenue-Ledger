package integrations

import (
"context"
"encoding/json"
"errors"
"fmt"
"time"

"github.com/jackc/pgx/v5"
"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("integration not found")

type Integration struct {
ID        string          `json:"id"`
TenantID  string          `json:"tenant_id"`
Provider  string          `json:"provider"`
Status    string          `json:"status"`
Config    json.RawMessage `json:"config"`
CreatedAt time.Time       `json:"created_at"`
UpdatedAt time.Time       `json:"updated_at"`
}

type integrationMeta struct {
Provider     string  `json:"provider"`
Status       string  `json:"status"`
LastSyncAt   string  `json:"last_sync_at"`
Stale        bool    `json:"stale"`
AccountLabel *string `json:"account_label,omitempty"`
Error        *string `json:"error,omitempty"`
}

type PGStore struct {
pool *pgxpool.Pool
}

func NewPGStore(pool *pgxpool.Pool) *PGStore {
return &PGStore{pool: pool}
}

func buildConfigJSON(provider, status string, lastSync time.Time, stale bool, label, lastErr *string) json.RawMessage {
meta := integrationMeta{
Provider:     provider,
Status:       status,
LastSyncAt:   lastSync.UTC().Format(time.RFC3339),
Stale:        stale,
AccountLabel: label,
Error:        lastErr,
}
b, _ := json.Marshal(meta)
return b
}

func (s *PGStore) ListByTenant(ctx context.Context, tenantID string) ([]Integration, error) {
const q = `
SELECT id::text, tenant_id, provider, status, last_sync_at, stale, account_label, last_error, updated_at
FROM integrations
WHERE tenant_id = $1
ORDER BY provider ASC
`
rows, err := s.pool.Query(ctx, q, tenantID)
if err != nil {
return nil, fmt.Errorf("list integrations: %w", err)
}
defer rows.Close()

var items []Integration
for rows.Next() {
var id, tid, provider, status string
var lastSync, updatedAt time.Time
var stale bool
var label, lastErr *string

if err := rows.Scan(&id, &tid, &provider, &status, &lastSync, &stale, &label, &lastErr, &updatedAt); err != nil {
return nil, fmt.Errorf("scan integration: %w", err)
}
items = append(items, Integration{
ID:        id,
TenantID:  tid,
Provider:  provider,
Status:    status,
Config:    buildConfigJSON(provider, status, lastSync, stale, label, lastErr),
CreatedAt: updatedAt,
UpdatedAt: updatedAt,
})
}
return items, rows.Err()
}

func (s *PGStore) UpsertForTenant(ctx context.Context, tenantID, provider, status string, cfg json.RawMessage) (Integration, error) {
var meta integrationMeta
if len(cfg) > 0 {
_ = json.Unmarshal(cfg, &meta)
}

lastSync := time.Now().UTC()
if meta.LastSyncAt != "" {
if parsed, err := time.Parse(time.RFC3339, meta.LastSyncAt); err == nil {
lastSync = parsed.UTC()
}
}

const q = `
INSERT INTO integrations (tenant_id, provider, status, last_sync_at, stale, account_label, last_error, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
ON CONFLICT (tenant_id, provider)
DO UPDATE SET
status = EXCLUDED.status,
last_sync_at = EXCLUDED.last_sync_at,
stale = EXCLUDED.stale,
account_label = EXCLUDED.account_label,
last_error = EXCLUDED.last_error,
updated_at = NOW()
RETURNING id::text, tenant_id, provider, status, last_sync_at, stale, account_label, last_error, updated_at
`
var id, tid, prov, st string
var syncAt, updatedAt time.Time
var stale bool
var label, lastErr *string

err := s.pool.QueryRow(ctx, q, tenantID, provider, status, lastSync, meta.Stale, meta.AccountLabel, meta.Error).Scan(
&id, &tid, &prov, &st, &syncAt, &stale, &label, &lastErr, &updatedAt,
)
if err != nil {
return Integration{}, fmt.Errorf("upsert integration: %w", err)
}
return Integration{
ID:        id,
TenantID:  tid,
Provider:  prov,
Status:    st,
Config:    buildConfigJSON(prov, st, syncAt, stale, label, lastErr),
CreatedAt: updatedAt,
UpdatedAt: updatedAt,
}, nil
}

func (s *PGStore) GetByIDForTenant(ctx context.Context, tenantID, id string) (Integration, error) {
const q = `
SELECT id::text, tenant_id, provider, status, last_sync_at, stale, account_label, last_error, updated_at
FROM integrations
WHERE id::text = $1 AND tenant_id = $2
`
var rowID, tid, provider, status string
var lastSync, updatedAt time.Time
var stale bool
var label, lastErr *string

err := s.pool.QueryRow(ctx, q, id, tenantID).Scan(
&rowID, &tid, &provider, &status, &lastSync, &stale, &label, &lastErr, &updatedAt,
)
if errors.Is(err, pgx.ErrNoRows) {
return Integration{}, ErrNotFound
}
if err != nil {
return Integration{}, fmt.Errorf("get integration: %w", err)
}
return Integration{
ID:        rowID,
TenantID:  tid,
Provider:  provider,
Status:    status,
Config:    buildConfigJSON(provider, status, lastSync, stale, label, lastErr),
CreatedAt: updatedAt,
UpdatedAt: updatedAt,
}, nil
}
