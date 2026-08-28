package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// foursquareAccountColumns is the select list every lookup shares, in the
// order [scanFoursquareAccount] reads them.
const foursquareAccountColumns = `user_id, foursquare_user_id, access_token, synced_through, created_at, updated_at`

// foursquareAccountRepository implements [store.FoursquareAccountRepository].
type foursquareAccountRepository struct {
	q querier
}

// Create implements [store.FoursquareAccountRepository].
func (r foursquareAccountRepository) Create(ctx context.Context, account model.FoursquareAccount) (model.FoursquareAccount, error) {
	// Truncated to the second for the reason on userRepository.Create: it is
	// what a later lookup will find.
	now := time.Now().UTC().Truncate(time.Second)

	_, err := r.q.ExecContext(ctx,
		`INSERT INTO foursquare_accounts (user_id, foursquare_user_id, access_token, synced_through, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		account.UserID, account.FoursquareUserID, account.AccessToken,
		nullUnixTimeValue(account.SyncedThrough), unixTime(now), unixTime(now),
	)
	if err != nil {
		return model.FoursquareAccount{}, fmt.Errorf(
			"sqlite: linking the Foursquare account for user %d: %w", account.UserID, translate(err))
	}

	account.CreatedAt = now
	account.UpdatedAt = now

	return account, nil
}

// ByFoursquareUserID implements [store.FoursquareAccountRepository].
func (r foursquareAccountRepository) ByFoursquareUserID(ctx context.Context, foursquareUserID string) (model.FoursquareAccount, error) {
	account, err := scanFoursquareAccount(r.q.QueryRowContext(ctx,
		`SELECT `+foursquareAccountColumns+` FROM foursquare_accounts WHERE foursquare_user_id = ?`,
		foursquareUserID,
	))
	if err != nil {
		return model.FoursquareAccount{}, fmt.Errorf("sqlite: looking up a Foursquare account: %w", err)
	}

	return account, nil
}

// scanFoursquareAccount reads one row of [foursquareAccountColumns].
func scanFoursquareAccount(row *sql.Row) (model.FoursquareAccount, error) {
	var (
		account              model.FoursquareAccount
		syncedThrough        sql.NullInt64
		createdAt, updatedAt unixTime
	)

	err := row.Scan(
		&account.UserID, &account.FoursquareUserID, &account.AccessToken,
		&syncedThrough, &createdAt, &updatedAt,
	)
	if err != nil {
		return model.FoursquareAccount{}, translate(err)
	}

	account.SyncedThrough = nullUnixTime(syncedThrough)
	account.CreatedAt = time.Time(createdAt)
	account.UpdatedAt = time.Time(updatedAt)

	return account, nil
}

// The interface this type exists to satisfy. See the equivalent assertion on
// [DB] for why this is worth spelling out.
var _ store.FoursquareAccountRepository = foursquareAccountRepository{}
