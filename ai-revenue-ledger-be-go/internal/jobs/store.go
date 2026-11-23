package jobs

import (
"context"
"fmt"
"net/http"
"sync"
"time"

"github.com/go-chi/chi/v5"
"github.com/your-org/ai-revenue-ledger-be-go/internal/auth"
"github.com/your-org/ai-revenue-ledger-be-go/internal/httputil"
)

type Job struct {
JobID             string  `json:"job_id"`
Type              string  `json:"type"`
TenantID          string  `json:"-"`
Status            string  `json:"status"` // queued, running, done, failed
Progress          float64 `json:"progress"`
StartedAt         string  `json:"started_at"`
EstimatedFinishAt string  `json:"estimated_finish_at"`
}

type Store struct {
mu      sync.RWMutex
seq     int
byID    map[string]*Job
running map[string]string // tenantID -> active jobID for recalculate
}

func NewStore() *Store {
return &Store{
seq:     789,
byID:    make(map[string]*Job),
running: make(map[string]string),
}
}

func (s *Store) Enqueue(jobType, tenantID string) string {
s.mu.Lock()
defer s.mu.Unlock()

s.seq++
id := fmt.Sprintf("job_%d", s.seq)
now := time.Now().UTC()
job := &Job{
JobID:             id,
Type:              jobType,
TenantID:          tenantID,
Status:            "queued",
Progress:          0.0,
StartedAt:         now.Format(time.RFC3339),
EstimatedFinishAt: now.Add(10 * time.Minute).Format(time.RFC3339),
}
s.byID[id] = job
if jobType == "recalculate" {
s.running[tenantID] = id
}
return id
}

func (s *Store) HasActiveRecalculate(tenantID string) bool {
s.mu.RLock()
defer s.mu.RUnlock()

id, ok := s.running[tenantID]
if !ok {
return false
}
job, exists := s.byID[id]
if !exists {
return false
}
return job.Status == "queued" || job.Status == "running"
}

func (s *Store) GetByID(id string) (*Job, bool) {
s.mu.RLock()
defer s.mu.RUnlock()
j, ok := s.byID[id]
if !ok {
return nil, false
}
copyJob := *j
return &copyJob, true
}

type Handler struct {
store *Store
pg    *PGStore
}

func NewHandler(store *Store) *Handler {
return &Handler{store: store}
}

func (h *Handler) SetPGStore(pg *PGStore) {
h.pg = pg
}

func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
claims, _ := auth.GetClaims(r.Context())
jobID := chi.URLParam(r, "job_id")

if h.pg != nil {
job, err := h.pg.GetByIDForTenant(r.Context(), jobID, claims.TenantID)
if err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load job", nil)
return
}
if job == nil {
httputil.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Job not found", map[string]any{
"job_id": jobID,
})
return
}
httputil.WriteJSON(w, http.StatusOK, job)
return
}

job, ok := h.store.GetByID(jobID)
if !ok {
httputil.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Job not found", map[string]any{
"job_id": jobID,
})
return
}
if job.TenantID != claims.TenantID {
httputil.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Access to job from another tenant is forbidden", nil)
return
}

httputil.WriteJSON(w, http.StatusOK, job)
}

func (h *Handler) EnqueueJob(ctx context.Context, jobType, tenantID string) (string, error) {
if h.pg != nil {
return h.pg.Enqueue(ctx, jobType, tenantID)
}
return h.store.Enqueue(jobType, tenantID), nil
}
