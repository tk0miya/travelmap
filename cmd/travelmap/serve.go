package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/tk0miya/travelmap/internal/config"
	"github.com/tk0miya/travelmap/internal/httpapi"
)

// serve runs the HTTP server until the process is asked to stop.
func serve(getenv func(string) string, stderr io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}

	logger := cfg.NewLogger(stderr)

	// SIGINT and SIGTERM are what a terminal and an init system send; both mean
	// "stop", so both start the graceful shutdown rather than killing requests.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting travelmap", "version", buildVersion())

	return httpapi.Serve(ctx, cfg.Addr, httpapi.New(httpapi.Options{Logger: logger}), logger)
}
