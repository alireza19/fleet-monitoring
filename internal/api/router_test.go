package api

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/alireza19/fleet-monitoring/internal/device"
	"github.com/alireza19/fleet-monitoring/internal/metrics"
)

// silenceChiLogger redirects log output during tests so the chi Logger
// middleware doesn't flood test output. Test isolation: deferred restore
// keeps other tests untouched.
func silenceChiLogger(t *testing.T) {
	t.Helper()
	prev := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(prev) })
}

func newProdRouter(t *testing.T) *httptest.Server {
	t.Helper()
	silenceChiLogger(t)
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
	r := NewRouter(&Handlers{Registry: reg, Store: store}, false /* debug */)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// TestRouter_MethodAndPath collapses cases 73–75. The shapes are uniform: a
// request that doesn't match a registered route should yield a specific
// status code, regardless of why.
func TestRouter_MethodAndPath(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{
			name:   "wrong method on heartbeat (POST-only)",
			method: http.MethodGet,
			path:   "/api/v1/devices/" + knownDevice + "/heartbeat",
			want:   http.StatusMethodNotAllowed,
		},
		{
			name:   "unknown subpath under devices",
			method: http.MethodPost,
			path:   "/api/v1/devices/" + knownDevice + "/unknown",
			want:   http.StatusNotFound,
		},
		{
			name:   "missing /api/v1 prefix",
			method: http.MethodPost,
			path:   "/devices/" + knownDevice + "/heartbeat",
			want:   http.StatusNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newProdRouter(t)
			req, err := http.NewRequest(tc.method, srv.URL+tc.path, strings.NewReader(`{}`))
			if err != nil {
				t.Fatalf("building request: %v", err)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("doing request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

// TestRouter_PanicRecovery (case 76): a panicking handler in our middleware
// stack should produce a 500 and the server should remain up. We mount a
// dedicated /boom route on a router built via the production middleware
// helper — that's the same stack NewRouter applies.
func TestRouter_PanicRecovery(t *testing.T) {
	silenceChiLogger(t)

	r := chi.NewRouter()
	applyDefaultMiddleware(r)
	r.Get("/boom", func(_ http.ResponseWriter, _ *http.Request) {
		panic("intentional test panic")
	})
	r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/boom")
	if err != nil {
		t.Fatalf("GET /boom: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("/boom status = %d, want 500", resp.StatusCode)
	}

	// Server still up after the panic.
	resp, err = srv.Client().Get(srv.URL + "/ping")
	if err != nil {
		t.Fatalf("GET /ping after panic: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/ping status = %d, want 200", resp.StatusCode)
	}
}
