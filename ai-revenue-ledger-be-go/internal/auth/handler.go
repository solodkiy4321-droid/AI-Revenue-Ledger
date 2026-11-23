package auth

import (
"net/http"

"github.com/your-org/ai-revenue-ledger-be-go/internal/httputil"
)

type Handler struct {
svc *Service
}

func NewHandler(svc *Service) *Handler {
return &Handler{svc: svc}
}

type loginRequest struct {
Email    string `json:"email"`
Password string `json:"password"`
TenantID string `json:"tenant_id"`
Role     Role   `json:"role"`
}

type loginResponse struct {
AccessToken  string `json:"access_token"`
RefreshToken string `json:"refresh_token"`
ExpiresIn    int    `json:"expires_in"`
Role         Role   `json:"role"`
TenantID     string `json:"tenant_id"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
var req loginRequest
if err := decodeJSONBody(r, &req); err != nil || req.Email == "" || req.Password == "" {
httputil.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "email and password are required", nil)
return
}

tenantID := req.TenantID
if tenantID == "" {
tenantID = "t_12345"
}
role := req.Role
if role == "" {
role = RoleOwner
}
if !role.Valid() {
httputil.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid role", nil)
return
}

access, refresh, expiresIn, err := h.svc.IssuePair(req.Email, tenantID, role)
if err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "VALIDATION_ERROR", "failed to issue token", nil)
return
}

httputil.WriteJSON(w, http.StatusOK, loginResponse{
AccessToken:  access,
RefreshToken: refresh,
ExpiresIn:    expiresIn,
Role:         role,
TenantID:     tenantID,
})
}

type refreshRequest struct {
RefreshToken string `json:"refresh_token"`
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
var req refreshRequest
if err := decodeJSONBody(r, &req); err != nil || req.RefreshToken == "" {
httputil.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "refresh_token is required", nil)
return
}

claims, err := h.svc.ParseAndValidate(req.RefreshToken, "refresh")
if err != nil {
httputil.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", err.Error(), nil)
return
}

access, refresh, expiresIn, err := h.svc.IssuePair(claims.Subject, claims.TenantID, claims.Role)
if err != nil {
httputil.WriteError(w, r, http.StatusInternalServerError, "VALIDATION_ERROR", "failed to refresh token", nil)
return
}

httputil.WriteJSON(w, http.StatusOK, loginResponse{
AccessToken:  access,
RefreshToken: refresh,
ExpiresIn:    expiresIn,
Role:         claims.Role,
TenantID:     claims.TenantID,
})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
var req refreshRequest
if err := decodeJSONBody(r, &req); err == nil && req.RefreshToken != "" {
if claims, err := h.svc.ParseAndValidate(req.RefreshToken, "refresh"); err == nil {
h.svc.Revoke(claims)
}
}
w.WriteHeader(http.StatusNoContent)
}
