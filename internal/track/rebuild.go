package track

import (
	"context"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/store"
)

// RebuildUser recomputes every one of userID's tracks from scratch and
// replaces whatever was stored before, in one transaction.
func RebuildUser(ctx context.Context, st store.Store, userID int64, trackBreak time.Duration) error {
	err := st.Tx(ctx, func(ctx context.Context, tx store.Store) error {
		points, err := tx.Points().AllOrdered(ctx, userID)
		if err != nil {
			return err
		}

		return tx.Tracks().ReplaceAll(ctx, userID, build(points, trackBreak))
	})
	if err != nil {
		return fmt.Errorf("track: rebuilding user %d: %w", userID, err)
	}

	return nil
}
