package reports

import (
"bytes"
"context"
"encoding/json"
"fmt"
"image"
"image/color"
"image/draw"
"image/png"
"net/http"
"strconv"
"strings"
"time"

"github.com/go-chi/chi/v5"
"github.com/your-org/ai-revenue-ledger-be-go/internal/auth"
"github.com/your-org/ai-revenue-ledger-be-go/internal/httputil"
"github.com/your-org/ai-revenue-ledger-be-go/internal/integrations"
"github.com/your-org/ai-revenue-ledger-be-go/internal/jobs"
"golang.org/x/image/font"
"golang.org/x/image/font/basicfont"
"golang.org/x/image/math/fixed"
)

type ConfirmedLayer struct {
Amount          int    `json:"amount"`
DealsCount      int    `json:"deals_count"`
ConfidenceLevel string `json:"confidence_level"`
Description     string `json:"description"`
}

type DeclaredLayer struct {
Amount          int    `json:"amount"`
DealsCount      int    `json:"deals_count"`
ConfidenceLevel string `json:"confidence_level"`
CoverageNote    string `json:"coverage_note"`
Description     string `json:"description"`
}

type ModeledLayer struct {
AmountLow       int    `json:"amount_low"`
AmountHigh      int    `json:"amount_high"`
ErrorMargin     string `json:"error_margin"`
ConfidenceLevel string `json:"confidence_level"`
Description     string `json:"description"`
}

type SummaryResponse struct {
TenantID           string `json:"tenant_id"`
Period             string `json:"period"`
GeneratedAt        string `json:"generated_at"`
MethodologyVersion string `json:"methodology_version"`
CalibrationDate    string `json:"calibration_date"`
Stale              bool   `json:"stale"`
Currency           string `json:"currency"`
Layers             struct {
Confirmed ConfirmedLayer `json:"confirmed"`
Declared  DeclaredLayer  `json:"declared"`
Modeled   ModeledLayer   `json:"modeled"`
} `json:"layers"`
Disclaimer string `json:"disclaimer"`
}

type reportHistoryMeta struct {
ReportID           string `json:"report_id"`
Period             string `json:"period"`
MethodologyVersion string `json:"methodology_version"`
}

type Handler struct {
jobs         *jobs.Store
pg           *PGStore
pgJobs       *jobs.PGStore
integrations *integrations.PGStore
}

func NewHandler(jobStore *jobs.Store) *Handler {
return &Handler{jobs: jobStore}
}

func (h *Handler) SetPGStore(pg *PGStore, pgJobs *jobs.PGStore) {
h.pg = pg
h.pgJobs = pgJobs
}

func (h *Handler) SetIntegrationsPGStore(pgIntegrations *integrations.PGStore) {
h.integrations = pgIntegrations
}

func validPeriod(p string) bool {
return p == "30d" || p == "90d" || p == "180d"
}

func periodDuration(p string) time.Duration {
switch p {
case "30d":
return 30 * 24 * time.Hour
case "180d":
return 180 * 24 * time.Hour
default:
return 90 * 24 * time.Hour
}
}

func (h *Handler) detectStale(ctx context.Context, tenantID string) (bool, string) {
fallbackTime := "2026-11-13T10:00:00Z"
if h.integrations == nil {
return false, fallbackTime
}
rows, err := h.integrations.ListByTenant(ctx, tenantID)
if err != nil || len(rows) == 0 {
return false, fallbackTime
}

stale := false
var latestSync time.Time
for _, row := range rows {
if row.Status == "error" || row.Status == "disconnected" {
stale = true
}
if len(row.Config) > 0 {
var cfg struct {
Stale      bool   `json:"stale"`
LastSyncAt string `json:"last_sync_at"`
}
if _ = json.Unmarshal(row.Config, &cfg); cfg.Stale {
stale = true
}
if cfg.LastSyncAt != "" {
if t, err := time.Parse(time.RFC3339, cfg.LastSyncAt); err == nil && t.After(latestSync) {
latestSync = t
}
}
}
if row.UpdatedAt.After(latestSync) {
latestSync = row.UpdatedAt
}
}

if !latestSync.IsZero() {
return stale, latestSync.UTC().Format(time.RFC3339)
}
return stale, fallbackTime
}

func (h *Handler) buildSummary(ctx context.Context, tenantID, period string) SummaryResponse {
stale, generatedAt := h.detectStale(ctx, tenantID)
version := "v1.0"

if h.pg != nil {
if rows, err := h.pg.ListByTenant(ctx, tenantID); err == nil {
for _, r := range rows {
var meta reportHistoryMeta
if len(r.Metrics) > 0 {
_ = json.Unmarshal(r.Metrics, &meta)
}
p := meta.Period
if p == "" {
p = "90d"
}
if p == period {
if meta.MethodologyVersion != "" {
version = meta.MethodologyVersion
}
if !r.CreatedAt.IsZero() && !stale {
generatedAt = r.CreatedAt.UTC().Format(time.RFC3339)
}
break
}
}
}
}

var resp SummaryResponse
resp.TenantID = tenantID
resp.Period = period
resp.GeneratedAt = generatedAt
resp.MethodologyVersion = version
resp.CalibrationDate = "2026-10-01"
resp.Stale = stale
resp.Currency = "USD"
resp.Layers.Confirmed = ConfirmedLayer{
Amount:          42000,
DealsCount:      14,
ConfidenceLevel: "confirmed",
Description:     "Sessions from AI assistants linked to closed deals",
}
resp.Layers.Declared = DeclaredLayer{
Amount:          18500,
DealsCount:      6,
ConfidenceLevel: "declared",
CoverageNote:    "37% of deals have source field filled",
Description:     "Deals where the customer stated AI as the source",
}
resp.Layers.Modeled = ModeledLayer{
AmountLow:       25000,
AmountHigh:      48000,
ErrorMargin:     "±30%",
ConfidenceLevel: "modeled",
Description:     "Conservative estimate of hidden influence",
}
resp.Disclaimer = "Modeled data is a conservative estimate, not precise attribution."
return resp
}

func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
claims, _ := auth.GetClaims(r.Context())
period := r.URL.Query().Get("period")
if period == "" {
period = "90d"
}
if !validPeriod(period) {
httputil.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid period; allowed values: 30d, 90d, 180d", map[string]any{
"period":  period,
"allowed": []string{"30d", "90d", "180d"},
})
return
}

httputil.WriteJSON(w, http.StatusOK, h.buildSummary(r.Context(), claims.TenantID, period))
}

func (h *Handler) Methodology(w http.ResponseWriter, r *http.Request) {
httputil.WriteJSON(w, http.StatusOK, map[string]any{
"version":     "v1.0",
"released_at": "2026-10-01",
"layers": map[string]any{
"confirmed": map[string]any{
"formula": "sessions where referrer in [chatgpt, perplexity, gemini, copilot] joined to deals on session_id",
"source":  "ga4 + hubspot",
},
"declared": map[string]any{
"formula": "deals where source_field in ['ai', 'chatgpt', ...]",
"source":  "hubspot",
},
"modeled": map[string]any{
"formula":     "confirmed_conversion_rate * avg_acv * brand_search_lift_factor",
"calibration": "monthly, last 90 days",
},
},
"verification": "SQL available in dbt models; contact support for access",
})
}

func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
claims, _ := auth.GetClaims(r.Context())
limit := 50
offset := 0
if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
v, err := strconv.Atoi(rawLimit)
if err != nil || v < 1 || v > 200 {
httputil.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid limit parameter", nil)
return
}
limit = v
}
if rawOffset := r.URL.Query().Get("offset"); rawOffset != "" {
v, err := strconv.Atoi(rawOffset)
if err != nil || v < 0 {
httputil.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid offset parameter", nil)
return
}
offset = v
}

var all []map[string]any
if h.pg != nil {
rows, err := h.pg.ListByTenant(r.Context(), claims.TenantID)
if err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load report history", nil)
return
}
for _, row := range rows {
var meta reportHistoryMeta
if len(row.Metrics) > 0 {
_ = json.Unmarshal(row.Metrics, &meta)
}
reportID := meta.ReportID
if reportID == "" {
reportID = row.ID
}
period := meta.Period
if period == "" {
period = "90d"
}
version := meta.MethodologyVersion
if version == "" {
version = "v1.0"
}
all = append(all, map[string]any{
"report_id":           reportID,
"generated_at":        row.CreatedAt.UTC().Format(time.RFC3339),
"methodology_version": version,
"period":              period,
})
}
} else {
all = []map[string]any{
{"report_id": "r_001", "generated_at": "2026-11-13T10:00:00Z", "methodology_version": "v1.0", "period": "90d"},
{"report_id": "r_000", "generated_at": "2026-10-13T10:00:00Z", "methodology_version": "v0.9", "period": "90d"},
}
}

total := len(all)
if offset >= total {
all = []map[string]any{}
} else {
end := offset + limit
if end > total {
end = total
}
all = all[offset:end]
}

httputil.WriteJSON(w, http.StatusOK, map[string]any{
"reports": all,
"total":   total,
})
}

func (h *Handler) Recalculate(w http.ResponseWriter, r *http.Request) {
claims, _ := auth.GetClaims(r.Context())

if h.pgJobs != nil {
active, err := h.pgJobs.HasActiveRecalculate(r.Context(), claims.TenantID)
if err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to check active recalculation", nil)
return
}
if active {
httputil.WriteError(w, r, http.StatusConflict, "CONFLICT", "Recalculation is already in progress for this tenant", nil)
return
}

jobID, err := h.pgJobs.Enqueue(r.Context(), "recalculate", claims.TenantID)
if err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to enqueue recalculation job", nil)
return
}

if h.pg != nil {
now := time.Now().UTC()
periodEnd := now
periodStart := now.Add(-periodDuration("90d"))
metricsJSON, _ := json.Marshal(reportHistoryMeta{
ReportID:           jobID,
Period:             "90d",
MethodologyVersion: "v1.0",
})
if _, err := h.pg.CreateForTenant(r.Context(), claims.TenantID, periodStart, periodEnd, metricsJSON); err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to persist report summary", nil)
return
}
}

httputil.WriteJSON(w, http.StatusAccepted, map[string]any{
"job_id":            jobID,
"status":            "queued",
"estimated_seconds": 600,
})
return
}

if h.jobs.HasActiveRecalculate(claims.TenantID) {
httputil.WriteError(w, r, http.StatusConflict, "CONFLICT", "Recalculation is already in progress for this tenant", nil)
return
}

jobID := h.jobs.Enqueue("recalculate", claims.TenantID)
httputil.WriteJSON(w, http.StatusAccepted, map[string]any{
"job_id":            jobID,
"status":            "queued",
"estimated_seconds": 600,
})
}

func buildReportLines(s SummaryResponse) []string {
staleLabel := "fresh"
if s.Stale {
staleLabel = "STALE (data may be outdated)"
}
return []string{
"AI REVENUE LEDGER - CFO EXECUTIVE SUMMARY",
fmt.Sprintf("Tenant: %s | Period: %s | Currency: %s | Status: %s", s.TenantID, s.Period, s.Currency, staleLabel),
fmt.Sprintf("Methodology: %s | Calibration: %s | Generated: %s", s.MethodologyVersion, s.CalibrationDate, s.GeneratedAt),
"--------------------------------------------------------------------------------",
fmt.Sprintf("1. CONFIRMED : $%d (%d deals)", s.Layers.Confirmed.Amount, s.Layers.Confirmed.DealsCount),
fmt.Sprintf("   %s", s.Layers.Confirmed.Description),
fmt.Sprintf("2. DECLARED  : $%d (%d deals) [%s]", s.Layers.Declared.Amount, s.Layers.Declared.DealsCount, s.Layers.Declared.CoverageNote),
fmt.Sprintf("   %s", s.Layers.Declared.Description),
fmt.Sprintf("3. MODELED   : $%d - $%d (%s)", s.Layers.Modeled.AmountLow, s.Layers.Modeled.AmountHigh, s.Layers.Modeled.ErrorMargin),
fmt.Sprintf("   %s", s.Layers.Modeled.Description),
"--------------------------------------------------------------------------------",
fmt.Sprintf("Disclaimer: %s", s.Disclaimer),
}
}

func renderPDF(lines []string) []byte {
var sb strings.Builder
sb.WriteString("BT /F1 11 Tf 40 780 Td 16 TL\n")
for i, line := range lines {
escaped := escapePDFText(line)
if i == 0 {
sb.WriteString(fmt.Sprintf("(%s) Tj\n", escaped))
} else {
sb.WriteString(fmt.Sprintf("T* (%s) Tj\n", escaped))
}
}
sb.WriteString("ET")
stream := sb.String()

return []byte(fmt.Sprintf(
"%%PDF-1.4\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 820] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >> endobj\n4 0 obj << /Length %d >> stream\n%s\nendstream endobj\n5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj\nxref\n0 6\n0000000000 65535 f \ntrailer << /Size 6 /Root 1 0 R >>\nstartxref\n0\n%%%%EOF",
len(stream),
stream,
))
}

func renderPNG(s SummaryResponse, lines []string) ([]byte, error) {
const w, h = 960, 540
img := image.NewRGBA(image.Rect(0, 0, w, h))

bg := color.RGBA{248, 250, 252, 255}
headerBg := color.RGBA{15, 23, 42, 255}
cardBg := color.RGBA{255, 255, 255, 255}
borderCol := color.RGBA{203, 213, 225, 255}
staleBg := color.RGBA{254, 243, 199, 255}
staleBorder := color.RGBA{217, 119, 6, 255}
textDark := color.RGBA{15, 23, 42, 255}
textMuted := color.RGBA{71, 85, 105, 255}
white := color.RGBA{255, 255, 255, 255}

draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
draw.Draw(img, image.Rect(0, 0, w, 72), &image.Uniform{headerBg}, image.Point{}, draw.Src)

drawText := func(x, y int, col color.Color, txt string) {
d := &font.Drawer{
Dst:  img,
Src:  image.NewUniform(col),
Face: basicfont.Face7x13,
Dot:  fixed.P(x, y),
}
d.DrawString(txt)
}

drawRect := func(x0, y0, x1, y1 int, fill, stroke color.Color) {
r := image.Rect(x0, y0, x1, y1)
if fill != nil {
draw.Draw(img, r, &image.Uniform{fill}, image.Point{}, draw.Src)
}
if stroke != nil {
draw.Draw(img, image.Rect(x0, y0, x1, y0+2), &image.Uniform{stroke}, image.Point{}, draw.Src)
draw.Draw(img, image.Rect(x0, y1-2, x1, y1), &image.Uniform{stroke}, image.Point{}, draw.Src)
draw.Draw(img, image.Rect(x0, y0, x0+2, y1), &image.Uniform{stroke}, image.Point{}, draw.Src)
draw.Draw(img, image.Rect(x1-2, y0, x1, y1), &image.Uniform{stroke}, image.Point{}, draw.Src)
}
}

drawText(28, 32, white, "AI REVENUE LEDGER - CFO SUMMARY")
drawText(28, 54, color.RGBA{148, 163, 184, 255}, fmt.Sprintf("Tenant: %s | Period: %s | Methodology: %s | Generated: %s",
s.TenantID, s.Period, s.MethodologyVersion, s.GeneratedAt))

if s.Stale {
drawRect(28, 86, w-28, 118, staleBg, staleBorder)
drawText(40, 106, staleBorder, "WARNING: STALE DATA - one or more integrations are disconnected, errored, or outdated")
}

topCardsY := 134
if !s.Stale {
topCardsY = 92
}

// Три карточки слоёв атрибуции.
cardW := (w - 28*2 - 24) / 3
c1x0, c1x1 := 28, 28+cardW
c2x0, c2x1 := c1x1+12, c1x1+12+cardW
c3x0, c3x1 := c2x1+12, c2x1+12+cardW

drawRect(c1x0, topCardsY, c1x1, topCardsY+150, cardBg, borderCol)
drawText(c1x0+14, topCardsY+26, textMuted, "CONFIRMED")
drawText(c1x0+14, topCardsY+56, textDark, fmt.Sprintf("$%d", s.Layers.Confirmed.Amount))
drawText(c1x0+14, topCardsY+80, textDark, fmt.Sprintf("%d deals", s.Layers.Confirmed.DealsCount))
drawText(c1x0+14, topCardsY+108, textMuted, "GA4 + HubSpot")

drawRect(c2x0, topCardsY, c2x1, topCardsY+150, cardBg, borderCol)
drawText(c2x0+14, topCardsY+26, textMuted, "DECLARED")
drawText(c2x0+14, topCardsY+56, textDark, fmt.Sprintf("$%d", s.Layers.Declared.Amount))
drawText(c2x0+14, topCardsY+80, textDark, fmt.Sprintf("%d deals", s.Layers.Declared.DealsCount))
drawText(c2x0+14, topCardsY+108, textMuted, s.Layers.Declared.CoverageNote)

drawRect(c3x0, topCardsY, c3x1, topCardsY+150, cardBg, borderCol)
drawText(c3x0+14, topCardsY+26, textMuted, "MODELED")
drawText(c3x0+14, topCardsY+56, textDark, fmt.Sprintf("$%d - $%d", s.Layers.Modeled.AmountLow, s.Layers.Modeled.AmountHigh))
drawText(c3x0+14, topCardsY+80, textDark, fmt.Sprintf("Margin: %s", s.Layers.Modeled.ErrorMargin))
drawText(c3x0+14, topCardsY+108, textMuted, fmt.Sprintf("Calibrated: %s", s.CalibrationDate))

// Детали и дисклеймер в нижней части.
detailY := topCardsY + 170
drawRect(28, detailY, w-28, h-24, cardBg, borderCol)
lineY := detailY + 22
for _, ln := range lines[4:] {
if lineY > h-34 {
break
}
drawText(40, lineY, textDark, ln)
lineY += 16
}

var buf bytes.Buffer
if err := png.Encode(&buf, img); err != nil {
return nil, err
}
return buf.Bytes(), nil
}

func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
claims, _ := auth.GetClaims(r.Context())
format := strings.ToLower(r.URL.Query().Get("format"))
if format == "" {
format = "pdf"
}
if format != "pdf" && format != "png" {
httputil.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid format; allowed: pdf, png", nil)
return
}

period := r.URL.Query().Get("period")
if period == "" {
period = "90d"
}
if !validPeriod(period) {
httputil.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid period; allowed: 30d, 90d, 180d", nil)
return
}

summary := h.buildSummary(r.Context(), claims.TenantID, period)
lines := buildReportLines(summary)

if format == "pdf" {
pdfBytes := renderPDF(lines)
w.Header().Set("Content-Type", "application/pdf")
w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"arl-report-%s.pdf\"", period))
w.WriteHeader(http.StatusOK)
_, _ = w.Write(pdfBytes)
return
}

pngBytes, err := renderPNG(summary, lines)
if err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to render PNG report", nil)
return
}
w.Header().Set("Content-Type", "image/png")
w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"arl-report-%s.png\"", period))
w.WriteHeader(http.StatusOK)
_, _ = w.Write(pngBytes)
}

func escapePDFText(s string) string {
s = strings.ReplaceAll(s, "\\", "\\\\")
s = strings.ReplaceAll(s, "(", "\\(")
s = strings.ReplaceAll(s, ")", "\\)")
s = strings.ReplaceAll(s, "\r", "")
s = strings.ReplaceAll(s, "\n", " ")
return s
}

var _ = chi.URLParam
