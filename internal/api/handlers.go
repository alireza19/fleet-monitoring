package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/alireza19/fleet-monitoring/internal/device"
	"github.com/alireza19/fleet-monitoring/internal/metrics"
)

// Handlers bundles the dependencies for HTTP handlers. DI through a struct
// rather than package globals — handlers are constructed in cmd/server/main
// and mounted on the router. Lets tests inject fakes via the same path.
type Handlers struct {
	Registry *device.Registry
	Store    *metrics.Store
}

// heartbeatBody uses pointer fields so we can distinguish "missing" from
// "zero-valued". time.Time has a non-trivial zero (year 1), but a pointer
// is the most legible signal: `nil` ⇒ field absent.
type heartbeatBody struct {
	SentAt *time.Time `json:"sent_at"`
}

type uploadStatsBody struct {
	SentAt     *time.Time `json:"sent_at"`
	UploadTime *int64     `json:"upload_time"`
}

type getStatsResponse struct {
	Uptime        float64 `json:"uptime"`
	AvgUploadTime string  `json:"avg_upload_time"`
}

// Heartbeat handles POST /devices/{device_id}/heartbeat.
func (h *Handlers) Heartbeat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "device_id")
	if !h.Registry.Has(id) {
		writeError(w, http.StatusNotFound, "device not found")
		return
	}

	var body heartbeatBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		// Per OpenAPI, malformed bodies are 500 (the spec only enumerates
		// 404/500 for these endpoints). HTTP-correct would be 400; we choose
		// spec conformance because the simulator is the test of record.
		writeError(w, http.StatusInternalServerError, "decoding request body: "+err.Error())
		return
	}
	if body.SentAt == nil {
		writeError(w, http.StatusInternalServerError, "missing required field: sent_at")
		return
	}

	h.Store.RecordHeartbeat(id, *body.SentAt)
	w.WriteHeader(http.StatusNoContent)
}

// PostStats handles POST /devices/{device_id}/stats.
func (h *Handlers) PostStats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "device_id")
	if !h.Registry.Has(id) {
		writeError(w, http.StatusNotFound, "device not found")
		return
	}

	var body uploadStatsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusInternalServerError, "decoding request body: "+err.Error())
		return
	}
	if body.SentAt == nil {
		writeError(w, http.StatusInternalServerError, "missing required field: sent_at")
		return
	}
	if body.UploadTime == nil {
		writeError(w, http.StatusInternalServerError, "missing required field: upload_time")
		return
	}

	// Spec doesn't forbid negative or zero upload_time. We record as-is rather
	// than rejecting; documented in the writeup as a deliberate design call.
	h.Store.RecordUpload(id, *body.UploadTime)
	w.WriteHeader(http.StatusNoContent)
}

// GetStats handles GET /devices/{device_id}/stats.
func (h *Handlers) GetStats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "device_id")
	if !h.Registry.Has(id) {
		writeError(w, http.StatusNotFound, "device not found")
		return
	}

	snap := h.Store.Snapshot(id)
	resp := getStatsResponse{
		Uptime:        metrics.Uptime(snap.ActiveMinutes, snap.FirstMinute, snap.LastMinute),
		AvgUploadTime: metrics.AvgUploadString(snap.UploadTotalNs, snap.UploadCount),
	}
	writeJSON(w, http.StatusOK, resp)
}
