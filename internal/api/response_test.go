package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteError_StatusBodyAndContentType(t *testing.T) {
	rr := httptest.NewRecorder()
	writeError(rr, http.StatusNotFound, "device not found")

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not JSON: %v", err)
	}
	if body.Msg != "device not found" {
		t.Errorf("msg = %q, want %q", body.Msg, "device not found")
	}
}

func TestWriteJSON_StatusBodyAndContentType(t *testing.T) {
	rr := httptest.NewRecorder()
	payload := map[string]any{
		"uptime":          60.0,
		"avg_upload_time": "5m10s",
	}
	writeJSON(rr, http.StatusOK, payload)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("response body is not JSON: %v", err)
	}
	if got["uptime"].(float64) != 60.0 || got["avg_upload_time"].(string) != "5m10s" {
		t.Errorf("body = %v, want uptime=60 avg_upload_time=5m10s", got)
	}
}
