package httputil

import (
"context"
"net/http"
"net/http/httptest"
"testing"
"time"
)

type testTenantKey struct{}

func TestEnforceTenantAndRateLimit(t *testing.T) {
rl := NewTenantRateLimiter(2, time.Minute)
extractTenant := func(ctx context.Context) string {
v, _ := ctx.Value(testTenantKey{}).(string)
return v
}
mw := EnforceTenantAndRateLimit(rl, extractTenant)

handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
WriteJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}))

ctx := context.WithValue(context.Background(), testTenantKey{}, "t_12345")

// 1. Попытка переопределить tenant_id из query должна вернуть 403.
reqOverride := httptest.NewRequest(http.MethodGet, "/v1/reports/summary?tenant_id=t_other", nil).WithContext(ctx)
recOverride := httptest.NewRecorder()
handler.ServeHTTP(recOverride, reqOverride)
if recOverride.Code != http.StatusForbidden {
t.Fatalf("expected 403 on tenant_id override, got %d", recOverride.Code)
}

// 2. Первые два запроса проходят и отдают заголовки rate limit.
for i := 0; i < 2; i++ {
req := httptest.NewRequest(http.MethodGet, "/v1/reports/summary", nil).WithContext(ctx)
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)
if rec.Code != http.StatusOK {
t.Fatalf("request %d expected 200, got %d", i+1, rec.Code)
}
if rec.Header().Get("X-RateLimit-Remaining") == "" || rec.Header().Get("X-RateLimit-Reset") == "" {
t.Fatalf("expected rate limit headers on request %d", i+1)
}
}

// 3. Третий запрос в рамках окна возвращает 429 RATE_LIMITED.
reqLimited := httptest.NewRequest(http.MethodGet, "/v1/reports/summary", nil).WithContext(ctx)
recLimited := httptest.NewRecorder()
handler.ServeHTTP(recLimited, reqLimited)
if recLimited.Code != http.StatusTooManyRequests {
t.Fatalf("expected 429 when rate limit exceeded, got %d", recLimited.Code)
}
}
