package api

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

// DebugBodyLogger logs the request body for POST /api/v1/devices/* requests.
// Used during simulator integration to diagnose mismatches between what the
// simulator sends and what we expect — wire format quirks (timestamps,
// number types) tend to surface here.
//
// The middleware reads the body, logs it, then restores r.Body via
// io.NopCloser(bytes.NewReader) so the downstream handler sees the same
// bytes. Status code is captured by chi's WrapResponseWriter.
//
// Only POST device paths are logged: GETs have no body and other paths are
// out of simulator scope. The check uses a string prefix so it is cheap on
// every request — no allocation, no chi dependency on the matcher.
func DebugBodyLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !shouldLogBody(r) {
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			// Read error is exotic (client disconnect mid-body). Log and
			// pass through with whatever we got — the handler will fail
			// the decode and produce its own error response.
			log.Printf("debug: reading body for %s %s: %v", r.Method, r.URL.Path, err)
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))

		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		log.Printf("debug: %s %s %d body=%s", r.Method, r.URL.Path, ww.Status(), string(body))
	})
}

func shouldLogBody(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/devices/")
}
