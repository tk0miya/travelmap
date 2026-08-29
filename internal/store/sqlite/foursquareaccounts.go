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

// All implements [store.FoursquareAccountRepository].
func (r foursquareAccountRepository) All(ctx context.Context) ([]model.FoursquareAccount, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+foursquareAccountColumns+` FROM foursquare_accounts ORDER BY user_id`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing the Foursquare accounts: %w", translate(err))
	}

	accounts, err := collect(rows, func(rows *sql.Rows) (model.FoursquareAccount, error) {
		return scanFoursquareAccount(rows)
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing the Foursquare accounts: %w", err)
	}

	return accounts, nil
}

// UpdateSyncedThrough implements [store.FoursquareAccountRepository].
func (r foursquareAccountRepository) UpdateSyncedThrough(ctx context.Context, userID int64, syncedThrough time.Time) error {
	// Not truncated to the second here: unixTime.Value() already stores
	// whole seconds, and nothing here hands a Go time back to a caller that
	// would need it to match — unlike Create, which returns account.
	result, err := r.q.ExecContext(ctx,
		`UPDATE foursquare_accounts SET synced_through = ?, updated_at = ? WHERE user_id = ?`,
		unixTime(syncedThrough), unixTime(time.Now()), userID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: recording the sync of user %d: %w", userID, translate(err))
	}

	// An update that matched nothing is the unlinked account this repository
	// reports as [store.ErrNotFound] everywhere else, not a silent success.
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: recording the sync of user %d: %w", userID, translate(err))
	}

	if affected == 0 {
		return fmt.Errorf("sqlite: recording the sync of user %d: %w", userID, store.ErrNotFound)
	}

	return nil
}

// rowScanner is the part of [sql.Row] and [sql.Rows] the scan below needs,
// so that a single-row lookup and the listing read the same columns through
// the same code.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanFoursquareAccount reads one row of [foursquareAccountColumns].
func scanFoursquareAccount(row rowScanner) (model.FoursquareAccount, error) {
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
