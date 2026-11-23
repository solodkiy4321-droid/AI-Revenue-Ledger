package reports

import (
"encoding/json"
"net/http"
"net/http/httptest"
"strings"
"testing"

"github.com/your-org/ai-revenue-ledger-be-go/internal/auth"
"github.com/your-org/ai-revenue-ledger-be-go/internal/jobs"
)

func mustAuthHeader(t *testing.T, svc *auth.Service, tenantID string, role auth.Role) string {
t.Helper()
accessToken, _, _, err := svc.IssuePair("u_test", tenantID, role)
if err != nil {
t.Fatalf("failed to issue token: %v", err)
}
return "Bearer " + accessToken
}

func TestSummaryAndExport(t *testing.T) {
svc, err := auth.NewService()
if err != nil {
t.Fatalf("failed to create auth service: %v", err)
}
jobStore := jobs.NewStore()
h := NewHandler(jobStore)
authHeader := mustAuthHeader(t, svc, "t_demo", auth.RoleOwner)

// 1. Проверка сводки за 90d.
req := httptest.NewRequest(http.MethodGet, "/v1/reports/summary?period=90d", nil)
req.Header.Set("Authorization", authHeader)
rec := httptest.NewRecorder()
svc.RequireAuth(http.HandlerFunc(h.Summary)).ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Fatalf("expected 200 for summary, got %d", rec.Code)
}

var sum SummaryResponse
if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil {
t.Fatalf("failed to decode summary json: %v", err)
}
if sum.TenantID != "t_demo" || sum.Period != "90d" {
t.Fatalf("unexpected tenant or period: %+v", sum)
}
if sum.MethodologyVersion == "" || sum.CalibrationDate == "" || sum.Disclaimer == "" {
t.Fatalf("expected methodology, calibration date, and disclaimer in summary: %+v", sum)
}

// 2. Проверка экспорта PDF и PNG.
for _, tc := range []struct {
format      string
contentType string
prefixCheck string
}{
{format: "pdf", contentType: "application/pdf", prefixCheck: "%PDF-1.4"},
{format: "png", contentType: "image/png", prefixCheck: "\x89PNG"},
} {
expReq := httptest.NewRequest(http.MethodGet, "/v1/reports/export?period=90d&format="+tc.format, nil)
expReq.Header.Set("Authorization", authHeader)
expRec := httptest.NewRecorder()
svc.RequireAuth(http.HandlerFunc(h.Export)).ServeHTTP(expRec, expReq)

if expRec.Code != http.StatusOK {
t.Fatalf("expected 200 for export %s, got %d", tc.format, expRec.Code)
}
if gotCT := expRec.Header().Get("Content-Type"); !strings.HasPrefix(gotCT, tc.contentType) {
t.Fatalf("expected content-type %s, got %s", tc.contentType, gotCT)
}
if !strings.HasPrefix(expRec.Body.String(), tc.prefixCheck) {
t.Fatalf("unexpected body prefix for %s", tc.format)
}
}

// 3. Невалидный период возвращает 400.
badReq := httptest.NewRequest(http.MethodGet, "/v1/reports/summary?period=7d", nil)
badReq.Header.Set("Authorization", authHeader)
badRec := httptest.NewRecorder()
svc.RequireAuth(http.HandlerFunc(h.Summary)).ServeHTTP(badRec, badReq)
if badRec.Code != http.StatusBadRequest {
t.Fatalf("expected 400 for invalid period, got %d", badRec.Code)
}
}
