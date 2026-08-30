package track

import (
	"context"
	"log/slog"
	"time"

	"github.com/tk0miya/travelmap/internal/store"
)

// pollInterval is how often [RunWorker] checks for a pending rebuild
// request. It is a constant rather than a setting: a shorter interval only
// costs a few more queries against a small table, and a track becoming
// visible a few seconds late is not a case worth a knob for.
const pollInterval = 5 * time.Second

// RunWorker drains pending track rebuild requests until ctx is done. It is
// started once, from cmd/travelmap/serve.go, alongside the signal-cancelled
// context that stops it — not joined there: cancelling ctx aborts a
// rebuild's own in-flight query rather than letting it finish, and
// RebuildUser's transaction then rolls back rather than partially writing,
// so an interrupted request is exactly what the next start's own catch-up
// resumes.
func RunWorker(ctx context.Context, st store.Store, trackBreak time.Duration, logger *slog.Logger) {
	// Catch up on anything left pending from before the last shutdown, or
	// enqueued while nothing was running to drain it, before waiting for the
	// first tick.
	drain(ctx, st, trackBreak, logger)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			drain(ctx, st, trackBreak, logger)
		}
	}
}

// drain claims and rebuilds every pending request in turn, stopping when
// none is left or ctx is done.
//
// A failure logs and stops the drain rather than looping again immediately
// against a database that just failed: the next tick retries, since claiming
// a request is what removed it from the queue, and RebuildUser's own
// transaction rolls the claim back along with the rebuild on failure.
func drain(ctx context.Context, st store.Store, trackBreak time.Duration, logger *slog.Logger) {
	for ctx.Err() == nil {
		var (
			userID int64
			ok     bool
		)

		err := st.Tx(ctx, func(ctx context.Context, tx store.Store) error {
			var err error

			userID, ok, err = tx.Tracks().NextPending(ctx)
			if err != nil || !ok {
				return err
			}

			// RebuildUser opens its own transaction, which joins this one
			// rather than starting a second one — see [store.Store.Tx] — so
			// the claim above and the rebuild below commit or roll back
			// together.
			return RebuildUser(ctx, tx, userID, trackBreak)
		})
		if err != nil {
			logger.Error("rebuilding tracks failed", "user_id", userID, "error", err)

			return
		}

		if !ok {
			return
		}
	}
}
