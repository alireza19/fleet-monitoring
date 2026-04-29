package api

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureLog redirects the stdlib logger into a buffer for the duration of
// the test and returns the buffer. Used to assert the middleware's log line.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := log.Writer()
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return buf
}

// echoHandler writes back the request body it received. Lets the test assert
// that the middleware restored r.Body correctly — the inner handler must
// still see the original payload.
func echoHandler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	w.WriteHeader(http.StatusNoContent)
	_, _ = w.Write(body)
}

func TestDebugBodyLogger_LogsPostDeviceBody(t *testing.T) {
	buf := captureLog(t)

	mw := DebugBodyLogger(http.HandlerFunc(echoHandler))
	body := `{"sent_at": "2026-01-01T14:23:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/abc/heartbeat", strings.NewReader(body))
	rr := httptest.NewRecorder()

	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rr.Code)
	}
	logged := buf.String()
	if !strings.Contains(logged, body) {
		t.Errorf("log %q does not contain body %q", logged, body)
	}
	if !strings.Contains(logged, "POST") || !strings.Contains(logged, "/api/v1/devices/abc/heartbeat") {
		t.Errorf("log %q missing method/path", logged)
	}
	if !strings.Contains(logged, "204") {
		t.Errorf("log %q missing status code", logged)
	}
}

// TestDebugBodyLogger_RestoresBody: the inner handler must still see the
// original body after the middleware reads it. The echoHandler writes the
// body back, so we can compare what the recorder received.
func TestDebugBodyLogger_RestoresBody(t *testing.T) {
	captureLog(t) // silence logger output to keep test logs clean

	mw := DebugBodyLogger(http.HandlerFunc(echoHandler))
	body := `{"upload_time": 12345}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/xyz/stats", strings.NewReader(body))
	rr := httptest.NewRecorder()

	mw.ServeHTTP(rr, req)

	if rr.Body.String() != body {
		t.Errorf("inner handler saw %q, want %q (body not restored)", rr.Body.String(), body)
	}
}

// TestDebugBodyLogger_SkipsNonDevicePaths: GET requests, healthz, paths
// outside /api/v1/devices/ should not trigger logging.
func TestDebugBodyLogger_SkipsNonDevicePaths(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "GET stats", method: http.MethodGet, path: "/api/v1/devices/abc/stats"},
		{name: "healthz", method: http.MethodGet, path: "/healthz"},
		{name: "POST outside devices", method: http.MethodPost, path: "/api/v1/something-else"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureLog(t)
			mw := DebugBodyLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			rr := httptest.NewRecorder()
			mw.ServeHTTP(rr, req)

			if buf.Len() != 0 {
				t.Errorf("logged for %s %s: %q (want no log)", tc.method, tc.path, buf.String())
			}
		})
	}
}
