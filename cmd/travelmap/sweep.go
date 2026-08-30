package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/tk0miya/travelmap/internal/store"
)

// sessionSweepInterval is how often expired sessions are purged. It is a
// constant rather than a setting: store.SessionRepository.ByToken already
// filters expired rows out itself, so a late sweep leaves no session usable —
// only rows on disk — and there is nothing here an operator could tune that
// would change observable behaviour.
const sessionSweepInterval = time.Hour

// sweepExpiredSessions deletes expired sessions on interval until ctx is
// cancelled. It is started as a goroutine from serve, the only place holding
// both the signal-cancelled context and the concrete store, and it stops on
// that same context so a sweep in flight is not left writing into a closing
// database.
func sweepExpiredSessions(ctx context.Context, st store.Store, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := st.Sessions().DeleteExpired(ctx); err != nil {
				logger.Error("sweeping expired sessions", "error", err)
			}
		}
	}
}
