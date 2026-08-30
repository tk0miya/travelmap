package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/tk0miya/travelmap/internal/config"
	"github.com/tk0miya/travelmap/internal/httpapi"
	"github.com/tk0miya/travelmap/internal/store/sqlite"
)

// serve runs the HTTP server until the process is asked to stop.
func serve(getenv func(string) string, stderr io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}

	logger := cfg.NewLogger(stderr)

	loc, err := cfg.Location()
	if err != nil {
		return err
	}

	// SIGINT and SIGTERM are what a terminal and an init system send; both mean
	// "stop", so both start the graceful shutdown rather than killing requests.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting travelmap", "version", buildVersion())

	db, err := sqlite.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return err
	}

	defer closeDatabase(db)

	// Before the listener opens, so that a server holding no history refuses
	// to come up rather than answering every request with a missing table.
	// See requireMigrated for why this is not simply migrated instead.
	if err := requireMigrated(ctx, db, cfg.DatabasePath); err != nil {
		return err
	}

	go sweepExpiredSessions(ctx, db, sessionSweepInterval, logger)

	handler := httpapi.New(httpapi.Options{
		Logger:                 logger,
		Store:                  db,
		DebugLogRequests:       cfg.DebugLogRequests,
		Timezone:               cfg.Timezone,
		Location:               loc,
		TrackBreak:             cfg.TrackBreak(),
		FoursquarePushSecret:   cfg.FoursquarePushSecret,
		FoursquareClientID:     cfg.FoursquareClientID,
		FoursquareClientSecret: cfg.FoursquareClientSecret,
		BaseURL:                cfg.BaseURL,
		FoursquareAPIURL:       cfg.FoursquareAPIURL,
		SessionLifetime:        cfg.SessionLifetime,
		SessionCookieSecure:    cfg.SessionCookieSecure,
	})

	return httpapi.Serve(ctx, cfg.Addr, handler, logger)
}
