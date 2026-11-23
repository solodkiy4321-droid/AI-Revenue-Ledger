package integrations

import (
"context"
"crypto/rand"
"encoding/hex"
"encoding/json"
"errors"
"fmt"
"net/http"
"sync"
"time"

"github.com/go-chi/chi/v5"
"github.com/your-org/ai-revenue-ledger-be-go/internal/auth"
"github.com/your-org/ai-revenue-ledger-be-go/internal/httputil"
"github.com/your-org/ai-revenue-ledger-be-go/internal/jobs"
)

type IntegrationItem struct {
Provider     string  `json:"provider"`
Status       string  `json:"status"`
LastSyncAt   string  `json:"last_sync_at"`
Stale        bool    `json:"stale"`
AccountLabel *string `json:"account_label,omitempty"`
Error        *string `json:"error,omitempty"`
}

type JobEnqueuer interface {
Enqueue(jobType, tenantID string) string
}

type Handler struct {
mu       sync.RWMutex
items    map[string]map[string]IntegrationItem
jobs     JobEnqueuer
pg       *PGStore
pgJobs   *jobs.PGStore
adapters map[string]ProviderAdapter
retryCfg RetryConfig
}

func NewHandler(jobs JobEnqueuer) *Handler {
return &Handler{
items:    make(map[string]map[string]IntegrationItem),
jobs:     jobs,
adapters: NewDefaultAdapters(),
retryCfg: DefaultRetryConfig(),
}
}

func (h *Handler) SetPGStore(pg *PGStore, pgJobs *jobs.PGStore) {
h.pg = pg
h.pgJobs = pgJobs
}

func defaultItems() map[string]IntegrationItem {
ga4Label := "Company GA4 Property 123"
gscLabel := "https://example.com"
hubspotErr := "OAuth token expired"
return map[string]IntegrationItem{
"ga4": {
Provider:     "ga4",
Status:       "connected",
LastSyncAt:   "2026-11-13T08:00:00Z",
Stale:        false,
AccountLabel: &ga4Label,
},
"gsc": {
Provider:     "gsc",
Status:       "connected",
LastSyncAt:   "2026-11-13T08:00:00Z",
Stale:        false,
AccountLabel: &gscLabel,
},
"hubspot": {
Provider:   "hubspot",
Status:     "error",
LastSyncAt: "2026-11-11T08:00:00Z",
Stale:      true,
Error:      &hubspotErr,
},
}
}

func itemToConfig(item IntegrationItem) json.RawMessage {
b, _ := json.Marshal(item)
return b
}

func itemFromRow(row Integration) IntegrationItem {
var item IntegrationItem
if len(row.Config) > 0 {
_ = json.Unmarshal(row.Config, &item)
}
if item.Provider == "" {
item.Provider = row.Provider
}
if item.Status == "" {
item.Status = row.Status
}
if item.LastSyncAt == "" {
item.LastSyncAt = row.UpdatedAt.UTC().Format(time.RFC3339)
}
return item
}

func generateState() string {
b := make([]byte, 8)
if _, err := rand.Read(b); err != nil {
return "state_default"
}
return "st_" + hex.EncodeToString(b)
}

func (h *Handler) ensureTenantPG(ctx context.Context, tenantID string) ([]IntegrationItem, error) {
rows, err := h.pg.ListByTenant(ctx, tenantID)
if err != nil {
return nil, err
}
if len(rows) == 0 {
defs := defaultItems()
for _, p := range []string{"ga4", "gsc", "hubspot"} {
it := defs[p]
if _, err := h.pg.UpsertForTenant(ctx, tenantID, it.Provider, it.Status, itemToConfig(it)); err != nil {
return nil, err
}
}
rows, err = h.pg.ListByTenant(ctx, tenantID)
if err != nil {
return nil, err
}
}

byProvider := make(map[string]IntegrationItem, len(rows))
for _, r := range rows {
byProvider[r.Provider] = itemFromRow(r)
}

out := make([]IntegrationItem, 0, len(byProvider))
for _, p := range []string{"ga4", "gsc", "hubspot"} {
if it, ok := byProvider[p]; ok {
out = append(out, it)
}
}
return out, nil
}

func (h *Handler) ensureTenant(tenantID string) map[string]IntegrationItem {
h.mu.Lock()
defer h.mu.Unlock()
if _, ok := h.items[tenantID]; !ok {
h.items[tenantID] = defaultItems()
}
return h.items[tenantID]
}

func isValidProvider(p string) bool {
return p == "ga4" || p == "gsc" || p == "hubspot"
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
claims, _ := auth.GetClaims(r.Context())

if h.pg != nil {
list, err := h.ensureTenantPG(r.Context(), claims.TenantID)
if err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load integrations", nil)
return
}
httputil.WriteJSON(w, http.StatusOK, map[string]any{
"integrations": list,
})
return
}

tenantMap := h.ensureTenant(claims.TenantID)

h.mu.RLock()
defer h.mu.RUnlock()

list := make([]IntegrationItem, 0, len(tenantMap))
for _, p := range []string{"ga4", "gsc", "hubspot"} {
if item, ok := tenantMap[p]; ok {
list = append(list, item)
}
}
httputil.WriteJSON(w, http.StatusOK, map[string]any{
"integrations": list,
})
}

func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
provider := chi.URLParam(r, "provider")
if !isValidProvider(provider) {
httputil.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Unsupported provider", map[string]any{
"provider": provider,
"allowed":  []string{"ga4", "gsc", "hubspot"},
})
return
}

claims, _ := auth.GetClaims(r.Context())
adapter := h.adapters[provider]
authURL := adapter.AuthorizationURL(claims.TenantID, generateState())
httputil.WriteJSON(w, http.StatusOK, map[string]string{
"authorization_url": authURL,
})
}

func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
provider := chi.URLParam(r, "provider")
if !isValidProvider(provider) {
httputil.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Unsupported provider", map[string]any{
"provider": provider,
})
return
}

claims, _ := auth.GetClaims(r.Context())
nowStr := time.Now().UTC().Format(time.RFC3339)
item := IntegrationItem{
Provider:   provider,
Status:     "disconnected",
LastSyncAt: nowStr,
Stale:      true,
}

if h.pg != nil {
if _, err := h.ensureTenantPG(r.Context(), claims.TenantID); err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to initialize integrations", nil)
return
}
if _, err := h.pg.UpsertForTenant(r.Context(), claims.TenantID, provider, "disconnected", itemToConfig(item)); err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to disconnect integration", nil)
return
}
httputil.WriteJSON(w, http.StatusOK, map[string]string{
"provider": provider,
"status":   "disconnected",
})
return
}

tenantMap := h.ensureTenant(claims.TenantID)

h.mu.Lock()
tenantMap[provider] = item
h.mu.Unlock()

httputil.WriteJSON(w, http.StatusOK, map[string]string{
"provider": provider,
"status":   "disconnected",
})
}

func (h *Handler) Sync(w http.ResponseWriter, r *http.Request) {
provider := chi.URLParam(r, "provider")
if !isValidProvider(provider) {
httputil.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Unsupported provider", map[string]any{
"provider": provider,
})
return
}

claims, _ := auth.GetClaims(r.Context())
_ = r.Body.Close()

var current IntegrationItem
incremental := false

if h.pg != nil {
list, err := h.ensureTenantPG(r.Context(), claims.TenantID)
if err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load integrations", nil)
return
}
for _, it := range list {
if it.Provider == provider {
current = it
break
}
}
} else {
tenantMap := h.ensureTenant(claims.TenantID)
h.mu.RLock()
current = tenantMap[provider]
h.mu.RUnlock()
}

if current.Status == "connected" && current.LastSyncAt != "" {
incremental = true
}

adapter := h.adapters[provider]
_, syncErr := DoWithRetry(r.Context(), h.retryCfg, func(ctx context.Context) (SyncResult, error) {
return adapter.FetchRaw(ctx, claims.TenantID, 90, incremental)
})

httputil.DefaultMetrics().RecordSync(provider, syncErr == nil)

nowStr := time.Now().UTC().Format(time.RFC3339)
updated := current
updated.Provider = provider
updated.LastSyncAt = nowStr

if syncErr != nil {
errMsg := syncErr.Error()
if errors.Is(syncErr, ErrTokenExpired) {
errMsg = "OAuth token expired"
}
updated.Status = "error"
updated.Stale = true
updated.Error = &errMsg

go httputil.SendAlert(fmt.Sprintf(
"[ARL Alert] Integration sync failed: provider=%s tenant_id=%s request_id=%s err=%s",
provider, claims.TenantID, httputil.GetRequestID(r.Context()), errMsg,
))
} else {
updated.Status = "connected"
updated.Stale = false
updated.Error = nil
}

if h.pg != nil {
if _, err := h.pg.UpsertForTenant(r.Context(), claims.TenantID, provider, updated.Status, itemToConfig(updated)); err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to persist integration status", nil)
return
}
} else {
tenantMap := h.ensureTenant(claims.TenantID)
h.mu.Lock()
tenantMap[provider] = updated
h.mu.Unlock()
}

if syncErr != nil {
code := "INTEGRATION_ERROR"
statusCode := http.StatusUnprocessableEntity
if errors.Is(syncErr, ErrTokenExpired) {
code = "INTEGRATION_TOKEN_EXPIRED"
} else if errors.Is(syncErr, ErrSourceDown) {
code = "SOURCE_UNAVAILABLE"
statusCode = http.StatusServiceUnavailable
}
httputil.WriteError(w, r, statusCode, code, errMsgOrDefault(syncErr), map[string]any{
"provider": provider,
"stale":    true,
})
return
}

var jobID string
if h.pgJobs != nil {
var err error
jobID, err = h.pgJobs.Enqueue(r.Context(), "sync_"+provider, claims.TenantID)
if err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to enqueue sync job", nil)
return
}
} else {
jobID = h.jobs.Enqueue("sync_"+provider, claims.TenantID)
}

httputil.WriteJSON(w, http.StatusAccepted, map[string]string{
"job_id": jobID,
"status": "queued",
})
}

func errMsgOrDefault(err error) string {
if errors.Is(err, ErrTokenExpired) {
return "OAuth token expired. Please reconnect."
}
if err == nil {
return "Integration sync failed"
}
return err.Error()
}
