package checkin

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/tk0miya/travelmap/internal/foursquare"
	"github.com/tk0miya/travelmap/internal/store"
)

// RunPeriodicSync repeats a sync run for every linked account on interval,
// until ctx is cancelled. It is started as a goroutine from
// cmd/travelmap/serve.go, the only place holding both the signal-cancelled
// context and the concrete store — which is also why that caller, not this
// function, decides whether to start it at all: interval <= 0
// (foursquare.sync_interval = "0") would panic inside time.NewTicker
// rather than mean anything to disable here.
func RunPeriodicSync(ctx context.Context, st store.Store, fetcher Fetcher, interval, lookback time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			syncAll(ctx, st, fetcher, lookback, logger)
		}
	}
}

// syncAll runs one sync for every linked account. One account's failure does
// not stop the run: it is logged and the rest still get their turn, since a
// revoked or rate-limited token says nothing about another account's.
func syncAll(ctx context.Context, st store.Store, fetcher Fetcher, lookback time.Duration, logger *slog.Logger) {
	accounts, err := st.FoursquareAccounts().All(ctx)
	if err != nil {
		logger.Error("listing linked Foursquare accounts", "error", err)

		return
	}

	for _, account := range accounts {
		if _, err := Sync(ctx, st, fetcher, account, lookback); err != nil {
			var apiErr *foursquare.APIError

			if errors.As(err, &apiErr) && apiErr.AuthorizationRevoked() {
				logger.Error("Foursquare authorisation revoked, reconnect Swarm from Settings",
					"user_id", account.UserID)

				continue
			}

			logger.Error("syncing Foursquare check-ins", "user_id", account.UserID, "error", err)
		}
	}
}
