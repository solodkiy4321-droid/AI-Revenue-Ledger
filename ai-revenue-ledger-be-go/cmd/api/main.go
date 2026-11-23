package main

import (
"context"
"log"
"net/http"
"time"

"github.com/go-chi/chi/v5"
"github.com/your-org/ai-revenue-ledger-be-go/internal/auth"
"github.com/your-org/ai-revenue-ledger-be-go/internal/config"
"github.com/your-org/ai-revenue-ledger-be-go/internal/db"
"github.com/your-org/ai-revenue-ledger-be-go/internal/httputil"
"github.com/your-org/ai-revenue-ledger-be-go/internal/integrations"
"github.com/your-org/ai-revenue-ledger-be-go/internal/jobs"
"github.com/your-org/ai-revenue-ledger-be-go/internal/reports"
)

func main() {
cfg := config.Load()
ctx := context.Background()

pgPool, err := db.Connect(ctx, cfg.DatabaseURL, cfg.DBTimeout)
if err != nil {
log.Fatalf("database connection failed: %v", err)
}
if pgPool != nil {
defer pgPool.Close()
log.Println("Connected to PostgreSQL")
} else {
log.Println("DATABASE_URL not set; running without PostgreSQL")
}

authSvc, err := auth.NewService()
if err != nil {
log.Fatalf("init auth service: %v", err)
}
authHandler := auth.NewHandler(authSvc)

jobStore := jobs.NewStore()
jobHandler := jobs.NewHandler(jobStore)
intHandler := integrations.NewHandler(jobStore)
repHandler := reports.NewHandler(jobStore)

if pgPool != nil {
pgJobs := jobs.NewPGStore(pgPool)
pgIntegrations := integrations.NewPGStore(pgPool)
pgReports := reports.NewPGStore(pgPool)

jobHandler.SetPGStore(pgJobs)
intHandler.SetPGStore(pgIntegrations, pgJobs)
repHandler.SetPGStore(pgReports, pgJobs)
repHandler.SetIntegrationsPGStore(pgIntegrations)
}

extractTenant := func(c context.Context) string {
if claims, ok := auth.GetClaims(c); ok {
return claims.TenantID
}
return ""
}

rateLimiter := httputil.NewTenantRateLimiter(100, time.Minute)

r := chi.NewRouter()
r.Use(httputil.RequestIDMiddleware)
r.Use(httputil.StructuredLogMiddleware(extractTenant))

r.NotFound(func(w http.ResponseWriter, r *http.Request) {
httputil.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found", nil)
})

r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
httputil.WriteError(w, r, http.StatusMethodNotAllowed, "VALIDATION_ERROR", "Method not allowed", nil)
})

r.Route("/v1", func(v1 chi.Router) {
v1.Get("/health", func(w http.ResponseWriter, r *http.Request) {
dbStatus := "memory_mode"
if pgPool != nil {
if err := pgPool.Ping(r.Context()); err != nil {
httputil.WriteError(w, r, http.StatusServiceUnavailable, "SOURCE_UNAVAILABLE", "PostgreSQL is unreachable", nil)
return
}
dbStatus = "connected"
}
httputil.WriteJSON(w, http.StatusOK, map[string]string{
"status": "ok",
"db":     dbStatus,
})
})

v1.Route("/auth", func(ar chi.Router) {
ar.Post("/login", authHandler.Login)
ar.Post("/refresh", authHandler.Refresh)
ar.Post("/logout", authHandler.Logout)

ar.Group(func(pr chi.Router) {
pr.Use(authSvc.RequireAuth)
pr.Get("/me", func(w http.ResponseWriter, r *http.Request) {
claims, _ := auth.GetClaims(r.Context())
httputil.WriteJSON(w, http.StatusOK, map[string]any{
"user":      claims.Subject,
"tenant_id": claims.TenantID,
"role":      claims.Role,
})
})
})
})

v1.Group(func(pr chi.Router) {
pr.Use(authSvc.RequireAuth)
pr.Use(httputil.EnforceTenantAndRateLimit(rateLimiter, extractTenant))

pr.With(auth.RequireRoles(auth.RoleOwner, auth.RoleAnalyst)).Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
httputil.WriteJSON(w, http.StatusOK, httputil.DefaultMetrics().Snapshot())
})

// Интеграции
pr.Route("/integrations", func(ir chi.Router) {
ir.With(auth.RequireRoles(auth.RoleOwner, auth.RoleAnalyst, auth.RoleViewer)).Get("/", intHandler.List)
ir.With(auth.RequireRoles(auth.RoleOwner)).Post("/{provider}/connect", intHandler.Connect)
ir.With(auth.RequireRoles(auth.RoleOwner)).Post("/{provider}/disconnect", intHandler.Disconnect)
ir.With(auth.RequireRoles(auth.RoleOwner, auth.RoleAnalyst)).Post("/{provider}/sync", intHandler.Sync)
})

// Отчёты
pr.Route("/reports", func(rr chi.Router) {
rr.With(auth.RequireRoles(auth.RoleOwner, auth.RoleAnalyst, auth.RoleViewer)).Get("/summary", repHandler.Summary)
rr.With(auth.RequireRoles(auth.RoleOwner, auth.RoleAnalyst, auth.RoleViewer)).Get("/methodology", repHandler.Methodology)
rr.With(auth.RequireRoles(auth.RoleOwner, auth.RoleAnalyst)).Get("/history", repHandler.History)
rr.With(auth.RequireRoles(auth.RoleOwner, auth.RoleAnalyst)).Post("/recalculate", repHandler.Recalculate)
rr.With(auth.RequireRoles(auth.RoleOwner, auth.RoleAnalyst, auth.RoleViewer)).Get("/export", repHandler.Export)
})

// Задачи
pr.With(auth.RequireRoles(auth.RoleOwner, auth.RoleAnalyst)).Get("/jobs/{job_id}", jobHandler.GetJob)
})
})

log.Printf("Starting API server on :%s", cfg.Port)
if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
log.Fatalf("server failed: %v", err)
}
}
