// Command server is the entry point for the fleet metrics HTTP service.
// It loads the device registry from a CSV file, initializes the in-memory
// metrics store, and serves the API on the configured port.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/alireza19/fleet-monitoring/internal/api"
	"github.com/alireza19/fleet-monitoring/internal/device"
	"github.com/alireza19/fleet-monitoring/internal/metrics"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func run() error {
	port := flag.Int("port", 6733, "HTTP listen port")
	csvPath := flag.String("csv", "devices.csv", "path to device manifest CSV")
	debug := flag.Bool("debug", false, "log incoming POST request bodies for /api/v1/devices/* paths")
	flag.Parse()

	reg, err := device.Load(*csvPath)
	if err != nil {
		return fmt.Errorf("loading registry: %w", err)
	}
	store := metrics.NewStore(reg.IDs())
	handlers := &api.Handlers{Registry: reg, Store: store}
	router := api.NewRouter(handlers, *debug)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", *port),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second, // mitigates Slowloris-style header attacks
	}

	// signal.NotifyContext gives us a context that is canceled on SIGINT or
	// SIGTERM. We use it to drive graceful shutdown: stop accepting new
	// connections, finish in-flight requests, then exit. The `stop` cleanup
	// releases the signal handler when run() returns.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Run the listener on its own goroutine so we can select on ctx.Done()
	// and the server error channel from the main goroutine.
	serveErr := make(chan error, 1)
	go func() {
		log.Printf("server: listening on %s", srv.Addr)
		// http.Server.ListenAndServe returns http.ErrServerClosed on graceful
		// shutdown — that is success, not an error.
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("listening: %w", err)
	case <-ctx.Done():
		log.Printf("server: shutdown signal received")
	}

	// Bounded shutdown so a stuck handler doesn't keep the process alive.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Printf("server: shutdown complete")
	return nil
}
