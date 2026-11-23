package reports

import (
"context"
"encoding/json"
"fmt"
"time"

"github.com/jackc/pgx/v5/pgxpool"
)

type ReportSummary struct {
ID          string          `json:"id"`
TenantID    string          `json:"tenant_id"`
PeriodStart time.Time       `json:"period_start"`
PeriodEnd   time.Time       `json:"period_end"`
Metrics     json.RawMessage `json:"metrics"`
CreatedAt   time.Time       `json:"created_at"`
}

type PGStore struct {
pool *pgxpool.Pool
}

func NewPGStore(pool *pgxpool.Pool) *PGStore {
return &PGStore{pool: pool}
}

func (s *PGStore) ListByTenant(ctx context.Context, tenantID string) ([]ReportSummary, error) {
const q = `
SELECT report_id, tenant_id, period, methodology_version, generated_at
FROM report_summaries
WHERE tenant_id = $1
ORDER BY generated_at DESC
`
rows, err := s.pool.Query(ctx, q, tenantID)
if err != nil {
return nil, fmt.Errorf("list report summaries: %w", err)
}
defer rows.Close()

var items []ReportSummary
for rows.Next() {
var reportID, tid, period, version string
var generatedAt time.Time
if err := rows.Scan(&reportID, &tid, &period, &version, &generatedAt); err != nil {
return nil, fmt.Errorf("scan report summary: %w", err)
}
metaBytes, _ := json.Marshal(reportHistoryMeta{
ReportID:           reportID,
Period:             period,
MethodologyVersion: version,
})
items = append(items, ReportSummary{
ID:        reportID,
TenantID:  tid,
PeriodEnd: generatedAt,
Metrics:   metaBytes,
CreatedAt: generatedAt,
})
}
return items, rows.Err()
}

func (s *PGStore) CreateForTenant(ctx context.Context, tenantID string, periodStart, periodEnd time.Time, metrics json.RawMessage) (ReportSummary, error) {
var meta reportHistoryMeta
if len(metrics) > 0 {
_ = json.Unmarshal(metrics, &meta)
}
reportID := meta.ReportID
if reportID == "" {
reportID = fmt.Sprintf("rep_%d", time.Now().UTC().UnixNano())
}
period := meta.Period
if period == "" {
period = "90d"
}
version := meta.MethodologyVersion
if version == "" {
version = "v1.0"
}

calibrationDate := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

const q = `
INSERT INTO report_summaries (
report_id, tenant_id, period, methodology_version, calibration_date,
stale, currency,
confirmed_amount, confirmed_deals,
declared_amount, declared_deals, declared_coverage_note,
modeled_amount_low, modeled_amount_high, modeled_error_margin,
disclaimer, generated_at
)
VALUES (
$1, $2, $3, $4, $5,
FALSE, 'USD',
42000, 14,
18500, 6, '37% of deals have source field filled',
25000, 48000, '±30%',
'Modeled data is a conservative estimate, not precise attribution.', $6
)
RETURNING report_id, tenant_id, generated_at
`
var outID, outTID string
var genAt time.Time
err := s.pool.QueryRow(ctx, q, reportID, tenantID, period, version, calibrationDate, periodEnd.UTC()).Scan(
&outID, &outTID, &genAt,
)
if err != nil {
return ReportSummary{}, fmt.Errorf("create report summary: %w", err)
}

outMetrics, _ := json.Marshal(reportHistoryMeta{
ReportID:           outID,
Period:             period,
MethodologyVersion: version,
})
return ReportSummary{
ID:          outID,
TenantID:    outTID,
PeriodStart: periodStart,
PeriodEnd:   genAt,
Metrics:     outMetrics,
CreatedAt:   genAt,
}, nil
}
