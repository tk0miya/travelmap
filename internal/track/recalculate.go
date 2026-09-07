package track

import (
	"context"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/store"
)

// RecalculateAll rebuilds every user's tracks from scratch. It is what
// `travelmap recalculate` runs, alongside internal/ingest's own Recalculate: a
// TRAVELMAP_TRACK_BREAK_MINUTES change touches no point, so it never reaches
// the enqueue internal/ingest does on a write, and this is the only path back
// to a consistent state.
//
// Each user is rebuilt in its own transaction, deliberately not the whole run
// in one, for the reason internal/ingest's Recalculate gives for doing the
// same: a single transaction would hold SQLite's one write lock for as long
// as the largest deployment's entire history takes to rebuild.
func RecalculateAll(ctx context.Context, st store.Store, trackBreak time.Duration) error {
	userIDs, err := st.Points().UserIDs(ctx)
	if err != nil {
		return fmt.Errorf("track: recalculating: %w", err)
	}

	for _, userID := range userIDs {
		if err := RebuildUser(ctx, st, userID, trackBreak); err != nil {
			return fmt.Errorf("track: recalculating user %d: %w", userID, err)
		}
	}

	return nil
}
