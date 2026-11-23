package httputil

import (
"fmt"
"net/http"
"sync"
)

type Metrics struct {
mu               sync.RWMutex
requestsTotal    int64
errors5xxTotal   int64
totalLatencyMs   int64
syncSuccessTotal map[string]int64
syncErrorTotal   map[string]int64
jobFailedTotal   int64
recalcDurationMs int64
recalcCount      int64
}

func NewMetrics() *Metrics {
return &Metrics{
syncSuccessTotal: make(map[string]int64),
syncErrorTotal:   make(map[string]int64),
}
}

func (m *Metrics) RecordRequest(status int, latencyMs int64) {
m.mu.Lock()
defer m.mu.Unlock()
m.requestsTotal++
if latencyMs > 0 {
m.totalLatencyMs += latencyMs
}
if status >= http.StatusInternalServerError {
m.errors5xxTotal++
}
}

func (m *Metrics) RecordSync(provider string, ok bool) {
m.mu.Lock()
defer m.mu.Unlock()
if ok {
m.syncSuccessTotal[provider]++
} else {
m.syncErrorTotal[provider]++
}
}

func (m *Metrics) RecordJobFailed(jobType string, durationMs int64) {
m.mu.Lock()
defer m.mu.Unlock()
m.jobFailedTotal++
if jobType == "recalculate" && durationMs > 0 {
m.recalcDurationMs += durationMs
m.recalcCount++
}
}

func (m *Metrics) RecordRecalcDone(durationMs int64) {
m.mu.Lock()
defer m.mu.Unlock()
if durationMs > 0 {
m.recalcDurationMs += durationMs
m.recalcCount++
}
}

func (m *Metrics) Snapshot() map[string]any {
m.mu.RLock()
defer m.mu.RUnlock()

var avgLatencyMs float64
if m.requestsTotal > 0 {
avgLatencyMs = float64(m.totalLatencyMs) / float64(m.requestsTotal)
}

var errorRate float64
if m.requestsTotal > 0 {
errorRate = float64(m.errors5xxTotal) / float64(m.requestsTotal)
}

var avgRecalcMs float64
if m.recalcCount > 0 {
avgRecalcMs = float64(m.recalcDurationMs) / float64(m.recalcCount)
}

syncOk := make(map[string]int64, len(m.syncSuccessTotal))
for k, v := range m.syncSuccessTotal {
syncOk[k] = v
}
syncErr := make(map[string]int64, len(m.syncErrorTotal))
for k, v := range m.syncErrorTotal {
syncErr[k] = v
}

return map[string]any{
"requests_total":     m.requestsTotal,
"errors_5xx_total":   m.errors5xxTotal,
"error_rate":         fmt.Sprintf("%.4f", errorRate),
"avg_latency_ms":     fmt.Sprintf("%.2f", avgLatencyMs),
"sync_success_total": syncOk,
"sync_error_total":   syncErr,
"job_failed_total":   m.jobFailedTotal,
"avg_recalc_ms":      fmt.Sprintf("%.2f", avgRecalcMs),
}
}
