package main

import (
	"context"
	"fmt"
	"io"

	"github.com/tk0miya/travelmap/internal/config"
	"github.com/tk0miya/travelmap/internal/ingest"
)

// recalculate rebuilds daily_stats for every user, from scratch. It is what
// an operator runs for recovery after an import or an inconsistency, and
// after TRAVELMAP_TIMEZONE or TRAVELMAP_TRACK_BREAK_MINUTES changes — see
// "Configuration affecting stored aggregates" in TODO.md.
func recalculate(getenv func(string) string, stdout io.Writer) error {
	ctx := context.Background()

	cfg, err := config.Load(getenv)
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

	fmt.Fprintf(stdout, "%s: daily_stats recalculated\n", path)

	return nil
}
