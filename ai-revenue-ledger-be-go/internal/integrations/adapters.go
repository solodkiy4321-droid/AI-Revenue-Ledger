package integrations

import (
"context"
"encoding/json"
"errors"
"fmt"
"net/http"
"os"
"strings"
"time"
)

var (
ErrTokenExpired = errors.New("oauth token expired")
ErrSourceDown   = errors.New("upstream source unavailable")
)

type SyncResult struct {
Provider     string         `json:"provider"`
Mode         string         `json:"mode"` // "initial_90d" | "incremental"
LookbackDays int            `json:"lookback_days"`
RecordsCount int            `json:"records_count"`
FetchedAt    time.Time      `json:"fetched_at"`
RawSample    map[string]any `json:"raw_sample,omitempty"`
}

type ProviderAdapter interface {
Provider() string
AuthorizationURL(tenantID, state string) string
FetchRaw(ctx context.Context, tenantID string, lookbackDays int, incremental bool) (SyncResult, error)
}

type RetryConfig struct {
MaxAttempts int
BaseDelay   time.Duration
MaxDelay    time.Duration
}

func DefaultRetryConfig() RetryConfig {
return RetryConfig{
MaxAttempts: 3,
BaseDelay:   150 * time.Millisecond,
MaxDelay:    2 * time.Second,
}
}

func DoWithRetry[T any](ctx context.Context, cfg RetryConfig, fn func(context.Context) (T, error)) (T, error) {
var zero T
if cfg.MaxAttempts < 1 {
cfg.MaxAttempts = 1
}
delay := cfg.BaseDelay
if delay <= 0 {
delay = 100 * time.Millisecond
}
maxDelay := cfg.MaxDelay
if maxDelay < delay {
maxDelay = delay
}

var lastErr error
for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
res, err := fn(ctx)
if err == nil {
return res, nil
}
lastErr = err

// Протухший токен требует переавторизации, ретраить бессмысленно.
if errors.Is(err, ErrTokenExpired) {
return zero, err
}
if attempt == cfg.MaxAttempts {
break
}

timer := time.NewTimer(delay)
select {
case <-ctx.Done():
timer.Stop()
return zero, ctx.Err()
case <-timer.C:
}

delay *= 2
if delay > maxDelay {
delay = maxDelay
}
}
return zero, lastErr
}

type HTTPAdapter struct {
provider   string
authBase   string
apiBaseEnv string
client     *http.Client
}

func NewDefaultAdapters() map[string]ProviderAdapter {
client := &http.Client{Timeout: 10 * time.Second}
return map[string]ProviderAdapter{
"ga4": &HTTPAdapter{
provider:   "ga4",
authBase:   "https://accounts.google.com/o/oauth2/v2/auth",
apiBaseEnv: "GA4_API_BASE_URL",
client:     client,
},
"gsc": &HTTPAdapter{
provider:   "gsc",
authBase:   "https://accounts.google.com/o/oauth2/v2/auth",
apiBaseEnv: "GSC_API_BASE_URL",
client:     client,
},
"hubspot": &HTTPAdapter{
provider:   "hubspot",
authBase:   "https://app.hubspot.com/oauth/authorize",
apiBaseEnv: "HUBSPOT_API_BASE_URL",
client:     client,
},
}
}

func (a *HTTPAdapter) Provider() string {
return a.provider
}

func (a *HTTPAdapter) AuthorizationURL(tenantID, state string) string {
clientID := os.Getenv(strings.ToUpper(a.provider) + "_OAUTH_CLIENT_ID")
if clientID == "" {
clientID = "arl-mvp-client-id"
}
redirectURI := os.Getenv("OAUTH_REDIRECT_URI")
if redirectURI == "" {
redirectURI = "http://localhost:8080/v1/integrations/" + a.provider + "/callback"
}
return fmt.Sprintf(
"%s?client_id=%s&redirect_uri=%s&response_type=code&state=%s&tenant_id=%s",
a.authBase, clientID, redirectURI, state, tenantID,
)
}

func (a *HTTPAdapter) FetchRaw(ctx context.Context, tenantID string, lookbackDays int, incremental bool) (SyncResult, error) {
if lookbackDays <= 0 || lookbackDays > 90 {
lookbackDays = 90
}
mode := "initial_90d"
if incremental {
mode = "incremental"
lookbackDays = 1
}

// Если задан реальный URL внешнего API в окружении, делаем HTTP-запрос за сырыми данными.
if baseURL := strings.TrimSpace(os.Getenv(a.apiBaseEnv)); baseURL != "" {
token := strings.TrimSpace(os.Getenv(strings.ToUpper(a.provider) + "_ACCESS_TOKEN"))
url := fmt.Sprintf("%s/raw?tenant_id=%s&days=%d", strings.TrimRight(baseURL, "/"), tenantID, lookbackDays)
req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
if err != nil {
return SyncResult{}, err
}
if token != "" {
req.Header.Set("Authorization", "Bearer "+token)
}

resp, err := a.client.Do(req)
if err != nil {
return SyncResult{}, fmt.Errorf("%w: %v", ErrSourceDown, err)
}
defer resp.Body.Close()

if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
return SyncResult{}, ErrTokenExpired
}
if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
return SyncResult{}, fmt.Errorf("%w: status %d", ErrSourceDown, resp.StatusCode)
}
if resp.StatusCode >= 400 {
return SyncResult{}, fmt.Errorf("upstream error: status %d", resp.StatusCode)
}

var payload map[string]any
_ = json.NewDecoder(resp.Body).Decode(&payload)

count := 100
if incremental {
count = 20
}
return SyncResult{
Provider:     a.provider,
Mode:         mode,
LookbackDays: lookbackDays,
RecordsCount: count,
FetchedAt:    time.Now().UTC(),
RawSample:    payload,
}, nil
}

// Режим по умолчанию для локальной разработки: возвращаем сырую выборку без бизнес-логики.
count := 90
if incremental {
count = 15
}
return SyncResult{
Provider:     a.provider,
Mode:         mode,
LookbackDays: lookbackDays,
RecordsCount: count,
FetchedAt:    time.Now().UTC(),
RawSample: map[string]any{
"source_adapter": a.provider,
"tenant_id":      tenantID,
"mode":           mode,
},
}, nil
}
