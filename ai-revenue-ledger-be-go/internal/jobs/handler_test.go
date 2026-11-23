package jobs

import (
"encoding/json"
"net/http"
"net/http/httptest"
"testing"

"github.com/go-chi/chi/v5"
"github.com/your-org/ai-revenue-ledger-be-go/internal/auth"
)

func TestJobEnqueueAndGetJob(t *testing.T) {
svc, err := auth.NewService()
if err != nil {
t.Fatalf("failed to create auth service: %v", err)
}
accessToken, _, _, err := svc.IssuePair("u_analyst", "t_demo", auth.RoleAnalyst)
if err != nil {
t.Fatalf("failed to issue token: %v", err)
}

store := NewStore()
h := NewHandler(store)

jobID := store.Enqueue("recalculate", "t_demo")
if jobID == "" {
t.Fatal("expected non-empty job ID")
}

r := chi.NewRouter()
r.Use(svc.RequireAuth)
r.Get("/v1/jobs/{job_id}", h.GetJob)

req := httptest.NewRequest(http.MethodGet, "/v1/jobs/"+jobID, nil)
req.Header.Set("Authorization", "Bearer "+accessToken)
rec := httptest.NewRecorder()
r.ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Fatalf("expected 200 for GetJob, got %d", rec.Code)
}

var body map[string]any
if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
t.Fatalf("failed to decode job response: %v", err)
}
if body["job_id"] != jobID {
t.Fatalf("expected job_id %s, got %v", jobID, body["job_id"])
}
}
