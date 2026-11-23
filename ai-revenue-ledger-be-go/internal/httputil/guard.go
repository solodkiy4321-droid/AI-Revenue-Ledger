package httputil

import (
"bytes"
"context"
"encoding/json"
"fmt"
"log"
"net/http"
"os"
"strconv"
"sync"
"time"
)

type TenantIDExtractor func(context.Context) string

var globalMetrics = NewMetrics()

func DefaultMetrics() *Metrics {
return globalMetrics
}

type tenantBucket struct {
windowStart time.Time
count       int
}

type TenantRateLimiter struct {
mu      sync.Mutex
limit   int
window  time.Duration
buckets map[string]*tenantBucket
}

func NewTenantRateLimiter(limit int, window time.Duration) *TenantRateLimiter {
if limit <= 0 {
limit = 100
}
if window <= 0 {
window = time.Minute
}
return &TenantRateLimiter{
limit:   limit,
window:  window,
buckets: make(map[string]*tenantBucket),
}
}

func (rl *TenantRateLimiter) Allow(tenantID string, now time.Time) (allowed bool, remaining int, resetUnix int64) {
rl.mu.Lock()
defer rl.mu.Unlock()

b, ok := rl.buckets[tenantID]
if !ok || now.Sub(b.windowStart) >= rl.window {
b = &tenantBucket{
windowStart: now,
count:       0,
}
rl.buckets[tenantID] = b
}

resetUnix = b.windowStart.Add(rl.window).Unix()
if b.count >= rl.limit {
return false, 0, resetUnix
}

b.count++
remaining = rl.limit - b.count
if remaining < 0 {
remaining = 0
}
return true, remaining, resetUnix
}

type statusRecorder struct {
http.ResponseWriter
status int
}

func (sr *statusRecorder) WriteHeader(code int) {
sr.status = code
sr.ResponseWriter.WriteHeader(code)
}

func SendAlert(message string) {
webhook := os.Getenv("ALERT_WEBHOOK_URL")
if webhook == "" {
return
}
payload, _ := json.Marshal(map[string]string{
"text": message,
})
req, err := http.NewRequest(http.MethodPost, webhook, bytes.NewReader(payload))
if err != nil {
return
}
req.Header.Set("Content-Type", "application/json")
client := &http.Client{Timeout: 3 * time.Second}
if resp, err := client.Do(req); err == nil {
_ = resp.Body.Close()
}
}

func StructuredLogMiddleware(extractTenant TenantIDExtractor) func(http.Handler) http.Handler {
return func(next http.Handler) http.Handler {
return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
start := time.Now()
rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

next.ServeHTTP(rec, r)

latencyMs := time.Since(start).Milliseconds()
globalMetrics.RecordRequest(rec.status, latencyMs)

reqID := GetRequestID(r.Context())
tenantID := ""
if extractTenant != nil {
tenantID = extractTenant(r.Context())
}

entry := map[string]any{
"ts":         time.Now().UTC().Format(time.RFC3339),
"request_id": reqID,
"tenant_id":  tenantID,
"method":     r.Method,
"path":       r.URL.Path,
"status":     rec.status,
"latency_ms": latencyMs,
}
if raw, err := json.Marshal(entry); err == nil {
log.Println(string(raw))
}

if rec.status >= http.StatusInternalServerError {
go SendAlert(fmt.Sprintf("[ARL Alert] %s %s returned %d (request_id=%s, tenant_id=%s)", r.Method, r.URL.Path, rec.status, reqID, tenantID))
}
})
}
}

func EnforceTenantAndRateLimit(rl *TenantRateLimiter, extractTenant TenantIDExtractor) func(http.Handler) http.Handler {
return func(next http.Handler) http.Handler {
return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
tenantID := ""
if extractTenant != nil {
tenantID = extractTenant(r.Context())
}
if tenantID == "" {
next.ServeHTTP(w, r)
return
}

if qTenant := r.URL.Query().Get("tenant_id"); qTenant != "" && qTenant != tenantID {
WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Cannot override tenant_id from query parameter", map[string]any{
"requested_tenant_id": qTenant,
})
return
}

allowed, remaining, resetUnix := rl.Allow(tenantID, time.Now().UTC())
w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetUnix, 10))

if !allowed {
WriteError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "Rate limit exceeded (100 requests/min per tenant)", nil)
return
}

next.ServeHTTP(w, r)
})
}
}
