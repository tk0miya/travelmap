package checkin

import (
	"context"
	"fmt"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// Write stores c, upserting on FoursquareCheckinID: push and the periodic
// fetch will both carry the same check-in, by design, so every write goes
// through here rather than a look-before-you-write in the caller. It returns
// the row as stored, with ID, CreatedAt and UpdatedAt filled in.
func Write(ctx context.Context, st store.Store, c model.Checkin) (model.Checkin, error) {
	stored, err := st.Checkins().Upsert(ctx, c)
	if err != nil {
		return model.Checkin{}, fmt.Errorf("checkin: writing %s: %w", c.FoursquareCheckinID, err)
	}

	return stored, nil
}
