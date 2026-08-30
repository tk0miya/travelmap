package main

import (
	"context"
	"fmt"
	"io"

	"github.com/tk0miya/travelmap/internal/config"
	"github.com/tk0miya/travelmap/internal/ingest"
	"github.com/tk0miya/travelmap/internal/track"
)

// recalculate rebuilds daily_stats and tracks for every user, from scratch.
// It is what an operator runs for recovery after an import or an
// inconsistency, and after the tracking.timezone or
// tracking.track_break_minutes settings change: neither touches a point, so
// neither reaches internal/ingest's own enqueue of a track rebuild, which
// fires only on a write.
func recalculate(configPath string, stdout io.Writer) error {
	ctx := context.Background()

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	loc, err := cfg.Location()
	if err != nil {
		return err
	}

	db, path, err := openConfiguredDatabase(ctx, cfg)
	if err != nil {
		return err
	}

	defer closeDatabase(db)

	if err := requireMigrated(ctx, db, path); err != nil {
		return err
	}

	if err := ingest.Recalculate(ctx, db, loc, cfg.TrackBreak()); err != nil {
		return err
	}

	if err := track.RecalculateAll(ctx, db, cfg.TrackBreak()); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%s: daily_stats and tracks recalculated\n", path)

	return nil
}
