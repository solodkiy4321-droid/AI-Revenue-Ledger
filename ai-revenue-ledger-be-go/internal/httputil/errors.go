package httputil

import (
"encoding/json"
"net/http"
)

type ErrorBody struct {
Code      string         `json:"code"`
Message   string         `json:"message"`
Details   map[string]any `json:"details"`
RequestID string         `json:"request_id"`
}

type ErrorResponse struct {
Error ErrorBody `json:"error"`
}

func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]any) {
if details == nil {
details = map[string]any{}
}
w.Header().Set("Content-Type", "application/json; charset=utf-8")
w.WriteHeader(status)
_ = json.NewEncoder(w).Encode(ErrorResponse{
Error: ErrorBody{
Code:      code,
Message:   message,
Details:   details,
RequestID: GetRequestID(r.Context()),
},
})
}

func WriteJSON(w http.ResponseWriter, status int, payload any) {
w.Header().Set("Content-Type", "application/json; charset=utf-8")
w.WriteHeader(status)
_ = json.NewEncoder(w).Encode(payload)
}
