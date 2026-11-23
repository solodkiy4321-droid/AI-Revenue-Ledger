package jobs

import (
"context"
"errors"
"fmt"
"os"
"strconv"
"sync"
"time"

"github.com/jackc/pgx/v5"
"github.com/jackc/pgx/v5/pgxpool"
"github.com/your-org/ai-revenue-ledger-be-go/internal/httputil"
)

type PGStore struct {
pool      *pgxpool.Pool
mu        sync.Mutex
seq       int
stepDelay time.Duration
}

func NewPGStore(pool *pgxpool.Pool) *PGStore {
startSeq := 789
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()

var maxNum int
const q = `
SELECT COALESCE(MAX(CAST(SUBSTRING(job_id FROM 5) AS INTEGER)), 789)
FROM jobs
WHERE job_id ~ '^job_[0-9]+$'
`
if err := pool.QueryRow(ctx, q).Scan(&maxNum); err == nil && maxNum > startSeq {
startSeq = maxNum
}

stepDelay := 2 * time.Second
if raw := os.Getenv("JOB_STEP_DELAY_MS"); raw != "" {
if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
stepDelay = time.Duration(ms) * time.Millisecond
}
}

return &PGStore{
pool:      pool,
seq:       startSeq,
stepDelay: stepDelay,
}
}

func (s *PGStore) Enqueue(ctx context.Context, jobType, tenantID string) (string, error) {
s.mu.Lock()
s.seq++
jobID := fmt.Sprintf("job_%d", s.seq)
s.mu.Unlock()

now := time.Now().UTC()
estFinishAt := now.Add(10 * time.Minute)

const q = `
INSERT INTO jobs (job_id, tenant_id, job_type, status, progress, started_at, estimated_finish_at, updated_at)
VALUES ($1, $2, $3, 'queued', 0.0, $4, $5, $4)
`
if _, err := s.pool.Exec(ctx, q, jobID, tenantID, jobType, now, estFinishAt); err != nil {
return "", fmt.Errorf("insert job: %w", err)
}

go s.runLifecycle(jobID, jobType, tenantID, now)
return jobID, nil
}

func (s *PGStore) runLifecycle(jobID, jobType, tenantID string, startedAt time.Time) {
if s.stepDelay > 0 {
time.Sleep(s.stepDelay)
}
if err := s.UpdateStatus(context.Background(), jobID, tenantID, "running", 0.45); err != nil {
durationMs := time.Since(startedAt).Milliseconds()
httputil.DefaultMetrics().RecordJobFailed(jobType, durationMs)
_ = s.UpdateStatus(context.Background(), jobID, tenantID, "failed", 0.45)
httputil.SendAlert(fmt.Sprintf("[ARL Alert] Job failed: job_id=%s type=%s tenant_id=%s err=%v", jobID, jobType, tenantID, err))
return
}

if s.stepDelay > 0 {
time.Sleep(s.stepDelay)
}
if err := s.UpdateStatus(context.Background(), jobID, tenantID, "done", 1.0); err != nil {
durationMs := time.Since(startedAt).Milliseconds()
httputil.DefaultMetrics().RecordJobFailed(jobType, durationMs)
_ = s.UpdateStatus(context.Background(), jobID, tenantID, "failed", 0.45)
httputil.SendAlert(fmt.Sprintf("[ARL Alert] Job failed: job_id=%s type=%s tenant_id=%s err=%v", jobID, jobType, tenantID, err))
return
}

if jobType == "recalculate" {
httputil.DefaultMetrics().RecordRecalcDone(time.Since(startedAt).Milliseconds())
}
}

func (s *PGStore) UpdateStatus(ctx context.Context, jobID, tenantID, status string, progress float64) error {
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()

const q = `
UPDATE jobs
SET status = $1,
    progress = $2,
    updated_at = NOW()
WHERE job_id = $3
  AND tenant_id = $4
`
_, err := s.pool.Exec(ctx, q, status, progress, jobID, tenantID)
return err
}

func (s *PGStore) HasActiveRecalculate(ctx context.Context, tenantID string) (bool, error) {
const q = `
SELECT EXISTS (
SELECT 1
FROM jobs
WHERE tenant_id = $1
  AND job_type = 'recalculate'
  AND status IN ('queued', 'running')
)
`
var exists bool
if err := s.pool.QueryRow(ctx, q, tenantID).Scan(&exists); err != nil {
return false, fmt.Errorf("check active recalculate: %w", err)
}
return exists, nil
}

func (s *PGStore) GetByIDForTenant(ctx context.Context, jobID, tenantID string) (*Job, error) {
const q = `
SELECT job_id, job_type, tenant_id, status, progress, started_at, estimated_finish_at
FROM jobs
WHERE tenant_id = $1
  AND job_id = $2
LIMIT 1
`
var j Job
var startedAt, estFinishAt time.Time
err := s.pool.QueryRow(ctx, q, tenantID, jobID).Scan(
&j.JobID,
&j.Type,
&j.TenantID,
&j.Status,
&j.Progress,
&startedAt,
&estFinishAt,
)
if errors.Is(err, pgx.ErrNoRows) {
return nil, nil
}
if err != nil {
return nil, fmt.Errorf("get job by id: %w", err)
}

j.StartedAt = startedAt.UTC().Format(time.RFC3339)
j.EstimatedFinishAt = estFinishAt.UTC().Format(time.RFC3339)
return &j, nil
}
