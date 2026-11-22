package models

import "time"

type Role string

const (
    RoleOwner   Role = "owner"
    RoleAnalyst Role = "analyst"
    RoleViewer  Role = "viewer"
)

type User struct {
    ID           int       `json:"id"`
    TenantID     string    `json:"tenant_id"`
    Email        string    `json:"email"`
    PasswordHash string    `json:"-"` 
    Role         Role      `json:"role"`
    CreatedAt    time.Time `json:"created_at"`
}

type Integration struct {
    ID           int       `json:"id"`
    TenantID     string    `json:"tenant_id"`
    Provider     string    `json:"provider"` 
    Status       string    `json:"status"`   
    LastSyncAt   *time.Time `json:"last_sync_at,omitempty"`
    ErrorMessage *string    `json:"error_message,omitempty"`
}

type Report struct {
    ID                 int       `json:"id"`
    TenantID           string    `json:"tenant_id"`
    ConfirmedAmount    float64   `json:"confirmed_amount"`
    DeclaredAmount     float64   `json:"declared_amount"`
    ModeledLow         float64   `json:"modeled_low"`
    ModeledHigh        float64   `json:"modeled_high"`
    MethodologyVersion string    `json:"methodology_version"`
    CalculatedAt       time.Time `json:"calculated_at"`
    Stale              bool      `json:"stale"`
}