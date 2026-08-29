package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// sessionColumns is the select list [scanSession] reads, in that order.
const sessionColumns = `token, data, expiry`

// sessionRepository implements [store.SessionRepository].
type sessionRepository struct {
	q querier
}

// ByToken implements [store.SessionRepository].
func (r sessionRepository) ByToken(ctx context.Context, token string) (model.Session, error) {
	session, err := scanSession(r.q.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE token = ? AND expiry > ?`,
		token, unixTime(time.Now().UTC()),
	))
	if err != nil {
		return model.Session{}, fmt.Errorf("sqlite: looking up a session: %w", err)
	}

	return session, nil
}

// Upsert implements [store.SessionRepository].
func (r sessionRepository) Upsert(ctx context.Context, session model.Session) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO sessions (token, data, expiry) VALUES (?, ?, ?)
		 ON CONFLICT (token) DO UPDATE SET data = excluded.data, expiry = excluded.expiry`,
		session.Token, session.Data, unixTime(session.Expiry),
	)
	if err != nil {
		return fmt.Errorf("sqlite: upserting a session: %w", translate(err))
	}

	return nil
}

// Delete implements [store.SessionRepository].
func (r sessionRepository) Delete(ctx context.Context, token string) error {
	if _, err := r.q.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token); err != nil {
		return fmt.Errorf("sqlite: deleting a session: %w", err)
	}

	return nil
}

// DeleteExpired implements [store.SessionRepository].
func (r sessionRepository) DeleteExpired(ctx context.Context) error {
	if _, err := r.q.ExecContext(ctx,
		`DELETE FROM sessions WHERE expiry <= ?`, unixTime(time.Now().UTC()),
	); err != nil {
		return fmt.Errorf("sqlite: deleting expired sessions: %w", err)
	}

	return nil
}

// scanSession reads one row of [sessionColumns].
func scanSession(row *sql.Row) (model.Session, error) {
	var (
		session model.Session
		expiry  unixTime
	)

	if err := row.Scan(&session.Token, &session.Data, &expiry); err != nil {
		return model.Session{}, translate(err)
	}

	session.Expiry = time.Time(expiry)

	return session, nil
}

// The interface this type exists to satisfy. See the equivalent assertion on
// [DB] for why this is worth spelling out.
var _ store.SessionRepository = sessionRepository{}
