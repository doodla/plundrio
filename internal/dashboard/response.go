package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/doodla/plundrio/internal/log"
)

// Stable machine error codes (contract §"Error response shape"). The dashboard
// has its own error envelope, distinct from the Transmission RPC {result,
// message} shape in internal/server/response.go.
const (
	codeValidationFailed = "validation_failed"
	codeKeyLocked        = "key_locked"
	codeNotFound         = "not_found"
	codeMethodNotAllowed = "method_not_allowed"
	codeUpstreamError    = "upstream_error"
)

// errorBody is the single REST error envelope: { "error": { code, message,
// field? } }. The body never includes the token or any secret, even in message.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// writeJSON serializes v as the response body with the JSON content type. On a
// marshal failure it logs and writes a 500 — a contract endpoint that can't
// serialize its own payload is a bug, not a client error.
func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Error("dashboard").Err(err).Msg("failed to marshal response")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"upstream_error","message":"internal serialization error"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// writeError emits the contract error envelope. field is optional (pass "" to
// omit it).
func writeError(w http.ResponseWriter, status int, code, message, field string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message, Field: field}})
}
