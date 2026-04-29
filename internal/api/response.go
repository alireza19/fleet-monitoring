// Package api owns the HTTP layer: handlers, response helpers, the chi
// router, and request/response middleware.
package api

import (
	"encoding/json"
	"log"
	"net/http"
)

// writeJSON marshals payload and writes it with the given status. Content-Type
// is set before WriteHeader because once status is written the headers are
// frozen — a common net/http footgun.
//
// Marshal errors are logged and the response is left in whatever state it's
// in: the status has already been sent, so we cannot meaningfully recover.
// In practice marshal errors require a programming bug (e.g., a non-marshalable
// type), not a runtime condition.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("api: marshaling response: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"msg":"internal error"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeError emits the {"msg": "..."} error envelope used everywhere in the
// API. Centralized so the response shape is impossible to drift on.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Msg: msg})
}

// errorBody is the envelope shape per the OpenAPI NotFoundResponse and
// ErrorResponse schemas.
type errorBody struct {
	Msg string `json:"msg"`
}
