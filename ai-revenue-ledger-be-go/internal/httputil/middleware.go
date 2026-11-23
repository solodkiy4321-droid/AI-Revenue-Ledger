package httputil

import (
"context"
"crypto/rand"
"encoding/hex"
"net/http"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

func newRequestID() string {
b := make([]byte, 6)
_, _ = rand.Read(b)
return "req_" + hex.EncodeToString(b)
}

func RequestIDMiddleware(next http.Handler) http.Handler {
return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
reqID := r.Header.Get("X-Request-ID")
if reqID == "" {
reqID = newRequestID()
}
w.Header().Set("X-Request-ID", reqID)
ctx := context.WithValue(r.Context(), requestIDKey, reqID)
next.ServeHTTP(w, r.WithContext(ctx))
})
}

func GetRequestID(ctx context.Context) string {
if v, ok := ctx.Value(requestIDKey).(string); ok && v != "" {
return v
}
return "req_unknown"
}
