package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/your-org/ai-revenue-ledger-be-go/internal/httputil"
)

type Role string

const (
	RoleOwner   Role = "owner"
	RoleAnalyst Role = "analyst"
	RoleViewer  Role = "viewer"
)

func (r Role) Valid() bool {
	return r == RoleOwner || r == RoleAnalyst || r == RoleViewer
}

type Claims struct {
	TenantID string `json:"tenant_id"`
	Role     Role   `json:"role"`
	TokenUse string `json:"token_use"`
	jwt.RegisteredClaims
}

type ctxKey string

const claimsCtxKey ctxKey = "auth_claims"

type Service struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	accessTTL  time.Duration
	refreshTTL time.Duration
	revokedJTI map[string]time.Time
}

func NewService() (*Service, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &Service{
		privateKey: priv,
		publicKey:  &priv.PublicKey,
		accessTTL:  time.Hour,
		refreshTTL: 30 * 24 * time.Hour,
		revokedJTI: make(map[string]time.Time),
	}, nil
}

func newJTI() string {
	b := make([]byte, 9)
	_, _ = rand.Read(b)
	return "jti_" + base64.RawURLEncoding.EncodeToString(b)
}

func (s *Service) IssuePair(subject, tenantID string, role Role) (accessToken, refreshToken string, expiresIn int, err error) {
	now := time.Now().UTC()

	accessClaims := Claims{
		TenantID: tenantID,
		Role:     role,
		TokenUse: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			ID:        newJTI(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
	}

	refreshClaims := Claims{
		TenantID: tenantID,
		Role:     role,
		TokenUse: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			ID:        newJTI(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.refreshTTL)),
		},
	}

	accessToken, err = jwt.NewWithClaims(jwt.SigningMethodRS256, accessClaims).SignedString(s.privateKey)
	if err != nil {
		return "", "", 0, err
	}
	refreshToken, err = jwt.NewWithClaims(jwt.SigningMethodRS256, refreshClaims).SignedString(s.privateKey)
	if err != nil {
		return "", "", 0, err
	}
	return accessToken, refreshToken, int(s.accessTTL.Seconds()), nil
}

func (s *Service) ParseAndValidate(tokenStr string, expectedUse string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodRS256 {
			return nil, errors.New("unexpected signing method")
		}
		return s.publicKey, nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid or expired token")
	}
	if claims.TokenUse != expectedUse {
		return nil, errors.New("invalid token use")
	}
	if claims.TenantID == "" || !claims.Role.Valid() {
		return nil, errors.New("missing tenant_id or invalid role")
	}
	if claims.ID != "" {
		if exp, revoked := s.revokedJTI[claims.ID]; revoked && time.Now().UTC().Before(exp) {
			return nil, errors.New("token revoked")
		}
	}
	return claims, nil
}

func (s *Service) Revoke(claims *Claims) {
	if claims != nil && claims.ID != "" {
		exp := time.Now().UTC().Add(s.refreshTTL)
		if claims.ExpiresAt != nil {
			exp = claims.ExpiresAt.Time
		}
		s.revokedJTI[claims.ID] = exp
	}
}

func GetClaims(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsCtxKey).(*Claims)
	return c, ok
}

func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			httputil.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing or invalid Bearer token", nil)
			return
		}
		rawToken := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		claims, err := s.ParseAndValidate(rawToken, "access")
		if err != nil {
			httputil.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", err.Error(), nil)
			return
		}

		queryTenant := r.URL.Query().Get("tenant_id")
		if queryTenant != "" && queryTenant != claims.TenantID {
			httputil.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Cannot override tenant_id from JWT", nil)
			return
		}

		ctx := context.WithValue(r.Context(), claimsCtxKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequireRoles(allowed ...Role) func(http.Handler) http.Handler {
	set := make(map[Role]struct{}, len(allowed))
	for _, r := range allowed {
		set[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetClaims(r.Context())
			if !ok {
				httputil.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required", nil)
				return
			}
			if _, allowedRole := set[claims.Role]; !allowedRole {
				httputil.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Insufficient role permissions", map[string]any{
					"required_roles": allowed,
					"current_role":   claims.Role,
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func decodeJSONBody(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	return dec.Decode(dst)
}