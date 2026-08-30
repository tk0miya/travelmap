package checkin

import (
	"context"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/foursquare"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// Fetcher is the part of the Foursquare client one sync run needs. It is
// named here, rather than the concrete client being taken, so that this
// package depends on the one call it makes; the client itself is what the
// tests below run against, through an httptest server.
type Fetcher interface {
	// EachCheckinPage walks the token holder's check-ins created at or after
	// after, newest first, calling fn with each page.
	EachCheckinPage(ctx context.Context, token string, after time.Time, fn func([]foursquare.Checkin) error) error
}

// Sync fetches account's check-ins from the last lookback of history and
// stores each of them through [Write], returning how many distinct check-ins
// the run collected.
//
// The window is computed from now on every run rather than resumed from
// account.SyncedThrough, and that is the point of it: a check-in can be added
// or edited after the fact, landing in the past of any high-water mark, and a
// cursor would skip it once and then forever. The overlap costs nothing —
// every check-in is written by upsert on its Foursquare id.
//
// A run can see the same check-in twice: paging walks a list by offset, and a
// check-in arriving mid-walk pushes one back across a boundary already read.
// The upsert would absorb the repeat anyway; skipping it here keeps the count
// a count of check-ins rather than of fetched items, which is what the caller
// reports.
//
// SyncedThrough is advanced to the run's own start only when the whole walk
// succeeded, so a run that could not page leaves behind a timestamp that
// still says how current the account actually is.
func Sync(ctx context.Context, st store.Store, fetcher Fetcher, account model.FoursquareAccount, lookback time.Duration) (int, error) {
	// Truncated to the second because that is the resolution the column
	// stores, and the one a check-in's own createdAt arrives in.
	now := time.Now().UTC().Truncate(time.Second)
	seen := make(map[string]struct{})

	err := fetcher.EachCheckinPage(ctx, account.AccessToken, now.Add(-lookback), func(page []foursquare.Checkin) error {
		for _, fetched := range page {
			if _, repeat := seen[fetched.ID]; repeat {
				continue
			}

			if _, err := Write(ctx, st, fromWire(account.UserID, SourceSync, fetched)); err != nil {
				return err
			}

			seen[fetched.ID] = struct{}{}
		}

		return nil
	})
	if err != nil {
		return len(seen), fmt.Errorf("checkin: syncing Foursquare user %s: %w", account.FoursquareUserID, err)
	}

	if err := st.FoursquareAccounts().UpdateSyncedThrough(ctx, account.UserID, now); err != nil {
		return len(seen), fmt.Errorf("checkin: recording the sync of Foursquare user %s: %w", account.FoursquareUserID, err)
	}

	return len(seen), nil
}
