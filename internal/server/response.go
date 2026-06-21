package server

import (
	"encoding/json"
	"net/http"

	"github.com/doodla/plundrio/internal/log"
)

// sendError sends an error response
func (s *Server) sendError(w http.ResponseWriter, err error) {
	log.Error("server").Msgf("Error processing request: %v", err)

	resp := struct {
		Result  string `json:"result"`
		Message string `json:"message,omitempty"`
	}{
		Result:  "error",
		Message: err.Error(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error("server").Msgf("Failed to encode error response: %v", err)
	}
}

// sendResponse sends a success response
func (s *Server) sendResponse(w http.ResponseWriter, tag interface{}, result interface{}) {
	// Create the response structure that matches what the Transmission client expects
	resp := struct {
		Tag       interface{} `json:"tag,omitempty"`
		Result    string      `json:"result"`
		Arguments interface{} `json:"arguments"`
	}{
		Tag:       tag,
		Result:    "success",
		Arguments: result,
	}

	// Log the response for debugging. Pass resp as a deferred field rather than
	// pre-marshaling: zerolog only serializes it when debug is actually enabled,
	// so the common case (info level, three *arrs polling torrent-get every few
	// seconds) no longer marshals the full torrent list twice per response.
	log.Debug("server").Interface("response", resp).Msg("Sending response")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Transmission-Session-Id", "123") // Ensure session ID is always sent
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error("server").Msgf("Failed to encode response: %v", err)
	}
}
