package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alireza19/fleet-monitoring/internal/device"
	"github.com/alireza19/fleet-monitoring/internal/metrics"
)

const (
	knownDevice   = "60-6b-44-84-dc-64"
	unknownDevice = "ff-ff-ff-ff-ff-ff"
)

// newTestServer builds a chi router with the real handler routes mounted.
// Tests issue real HTTP requests against an httptest.Server; this exercises
// path-param extraction and middleware ordering the same way production does.
func newTestServer(t *testing.T) (*httptest.Server, *metrics.Store, *device.Registry) {
	t.Helper()
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "devices.csv")
	if err := writeFile(csvPath, "device_id\n"+knownDevice+"\n"); err != nil {
		t.Fatalf("writing csv: %v", err)
	}
	reg, err := device.Load(csvPath)
	if err != nil {
		t.Fatalf("loading registry: %v", err)
	}
	store := metrics.NewStore([]string{knownDevice})

	silenceChiLogger(t)
	h := &Handlers{Registry: reg, Store: store}
	r := NewRouter(h, false /* debug */)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, store, reg
}

func do(t *testing.T, srv *httptest.Server, method, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("doing request: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// --- Heartbeat ---

func TestHeartbeat_HappyPath(t *testing.T) {
	srv, _, _ := newTestServer(t)
	body := `{"sent_at": "2026-01-01T14:23:00Z"}`

	resp := do(t, srv, http.MethodPost, "/api/v1/devices/"+knownDevice+"/heartbeat", body)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	if buf.Len() != 0 {
		t.Errorf("body = %q, want empty", buf.String())
	}
}

func TestHeartbeat_UnknownDevice(t *testing.T) {
	srv, _, _ := newTestServer(t)
	body := `{"sent_at": "2026-01-01T14:23:00Z"}`

	resp := do(t, srv, http.MethodPost, "/api/v1/devices/"+unknownDevice+"/heartbeat", body)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	var got struct {
		Msg string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if got.Msg != "device not found" {
		t.Errorf("msg = %q, want %q", got.Msg, "device not found")
	}
}

// TestHeartbeat_MalformedBodies groups cases 50-54: every malformed-body
// variant should produce 500 with the {msg} envelope. We don't distinguish
// between flavors of malformed because the decoder treats them uniformly.
func TestHeartbeat_MalformedBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed json", body: `{`},
		{name: "missing sent_at", body: `{}`},
		{name: "wrong field type", body: `{"sent_at": 12345}`},
		{name: "invalid date string", body: `{"sent_at": "not-a-date"}`},
		{name: "empty body", body: ``},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, _ := newTestServer(t)
			resp := do(t, srv, http.MethodPost, "/api/v1/devices/"+knownDevice+"/heartbeat", tc.body)
			if resp.StatusCode != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", resp.StatusCode)
			}
			var got struct {
				Msg string `json:"msg"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatalf("decoding body: %v", err)
			}
			if got.Msg == "" {
				t.Error("msg field is empty")
			}
		})
	}
}

// TestHeartbeat_SideEffect: POST a heartbeat, then GET stats and confirm
// uptime > 0. This verifies the wire-to-store path actually persists.
func TestHeartbeat_SideEffect(t *testing.T) {
	srv, _, _ := newTestServer(t)
	body := `{"sent_at": "2026-01-01T14:23:00Z"}`
	resp := do(t, srv, http.MethodPost, "/api/v1/devices/"+knownDevice+"/heartbeat", body)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("heartbeat status = %d, want 204", resp.StatusCode)
	}

	resp = do(t, srv, http.MethodGet, "/api/v1/devices/"+knownDevice+"/stats", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats status = %d, want 200", resp.StatusCode)
	}
	var got struct {
		Uptime        float64 `json:"uptime"`
		AvgUploadTime string  `json:"avg_upload_time"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if got.Uptime != 100.0 {
		t.Errorf("uptime = %v, want 100 (single heartbeat → span 1, count 1)", got.Uptime)
	}
}

// --- POST /devices/{id}/stats ---

func TestPostStats_HappyPath(t *testing.T) {
	srv, _, _ := newTestServer(t)
	body := `{"sent_at": "2026-01-01T14:23:00Z", "upload_time": 5000000000}`

	resp := do(t, srv, http.MethodPost, "/api/v1/devices/"+knownDevice+"/stats", body)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

func TestPostStats_UnknownDevice(t *testing.T) {
	srv, _, _ := newTestServer(t)
	body := `{"sent_at": "2026-01-01T14:23:00Z", "upload_time": 5000000000}`

	resp := do(t, srv, http.MethodPost, "/api/v1/devices/"+unknownDevice+"/stats", body)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestPostStats_MalformedBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed json", body: `{`},
		{name: "missing upload_time", body: `{"sent_at": "2026-01-01T14:23:00Z"}`},
		{name: "missing sent_at", body: `{"upload_time": 1000}`},
		{name: "wrong upload_time type", body: `{"sent_at": "2026-01-01T14:23:00Z", "upload_time": "fast"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, _ := newTestServer(t)
			resp := do(t, srv, http.MethodPost, "/api/v1/devices/"+knownDevice+"/stats", tc.body)
			if resp.StatusCode != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", resp.StatusCode)
			}
		})
	}
}

// Cases 62-63: zero and negative upload_time accepted. Spec doesn't forbid
// either; we record as-is and document the choice in the writeup.
func TestPostStats_EdgeValuesAccepted(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "zero upload_time", body: `{"sent_at": "2026-01-01T14:23:00Z", "upload_time": 0}`},
		{name: "negative upload_time", body: `{"sent_at": "2026-01-01T14:23:00Z", "upload_time": -100}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, _ := newTestServer(t)
			resp := do(t, srv, http.MethodPost, "/api/v1/devices/"+knownDevice+"/stats", tc.body)
			if resp.StatusCode != http.StatusNoContent {
				t.Errorf("status = %d, want 204", resp.StatusCode)
			}
		})
	}
}

func TestPostStats_SideEffect(t *testing.T) {
	srv, _, _ := newTestServer(t)
	body := `{"sent_at": "2026-01-01T14:23:00Z", "upload_time": 100000000}` // 100ms
	resp := do(t, srv, http.MethodPost, "/api/v1/devices/"+knownDevice+"/stats", body)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("post status = %d, want 204", resp.StatusCode)
	}

	resp = do(t, srv, http.MethodGet, "/api/v1/devices/"+knownDevice+"/stats", "")
	var got struct {
		Uptime        float64 `json:"uptime"`
		AvgUploadTime string  `json:"avg_upload_time"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if got.AvgUploadTime != "100ms" {
		t.Errorf("avg_upload_time = %q, want %q", got.AvgUploadTime, "100ms")
	}
}

// --- GET /devices/{id}/stats ---

func TestGetStats_EmptyState(t *testing.T) {
	srv, _, _ := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/devices/"+knownDevice+"/stats", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got struct {
		Uptime        float64 `json:"uptime"`
		AvgUploadTime string  `json:"avg_upload_time"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if got.Uptime != 0 || got.AvgUploadTime != "0s" {
		t.Errorf("got %+v, want uptime=0 avg_upload_time=0s", got)
	}
}

func TestGetStats_UnknownDevice(t *testing.T) {
	srv, _, _ := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/devices/"+unknownDevice+"/stats", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetStats_HeartbeatsOnly(t *testing.T) {
	srv, _, _ := newTestServer(t)
	// 7 distinct minutes (0..5 and 10) over a 10-minute span (last-first=10).
	// Under the simulator's denominator (max-min), uptime = 7/10 = 70%.
	for _, m := range []int{0, 1, 2, 3, 4, 5} {
		body := `{"sent_at": "2026-01-01T14:0` + itoaPad(m) + `:00Z"}`
		do(t, srv, http.MethodPost, "/api/v1/devices/"+knownDevice+"/heartbeat", body)
	}
	body := `{"sent_at": "2026-01-01T14:10:00Z"}`
	do(t, srv, http.MethodPost, "/api/v1/devices/"+knownDevice+"/heartbeat", body)

	resp := do(t, srv, http.MethodGet, "/api/v1/devices/"+knownDevice+"/stats", "")
	var got struct {
		Uptime        float64 `json:"uptime"`
		AvgUploadTime string  `json:"avg_upload_time"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got.Uptime != 70.0 {
		t.Errorf("uptime = %v, want 70", got.Uptime)
	}
	if got.AvgUploadTime != "0s" {
		t.Errorf("avg = %q, want 0s", got.AvgUploadTime)
	}
}

func TestGetStats_StatsOnly(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for range 3 {
		body := `{"sent_at": "2026-01-01T14:23:00Z", "upload_time": 100000000}`
		do(t, srv, http.MethodPost, "/api/v1/devices/"+knownDevice+"/stats", body)
	}
	resp := do(t, srv, http.MethodGet, "/api/v1/devices/"+knownDevice+"/stats", "")
	var got struct {
		Uptime        float64 `json:"uptime"`
		AvgUploadTime string  `json:"avg_upload_time"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got.Uptime != 0 {
		t.Errorf("uptime = %v, want 0", got.Uptime)
	}
	if got.AvgUploadTime != "100ms" {
		t.Errorf("avg = %q, want 100ms", got.AvgUploadTime)
	}
}

// TestGetStats_ResponseShape: uptime is JSON number, avg_upload_time is JSON
// string. Decode into a map[string]any to inspect the raw types.
func TestGetStats_ResponseShape(t *testing.T) {
	srv, _, _ := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/devices/"+knownDevice+"/stats", "")
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if _, ok := raw["uptime"].(float64); !ok {
		t.Errorf("uptime is %T, want float64 (JSON number)", raw["uptime"])
	}
	if _, ok := raw["avg_upload_time"].(string); !ok {
		t.Errorf("avg_upload_time is %T, want string", raw["avg_upload_time"])
	}
}

// --- /healthz ---

func TestHealthz(t *testing.T) {
	srv, _, _ := newTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if got.Status != "ok" {
		t.Errorf("status field = %q, want ok", got.Status)
	}
}

// itoaPad returns "0", "1", ..., "9" — used to build minute strings in the
// heartbeats-only test. Kept inline rather than pulling fmt.Sprintf to avoid
// the format-string call in a hot test loop. Trivial helper.
func itoaPad(n int) string {
	return string(rune('0' + n))
}
