package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// testSession builds a session as scs would commit one, expiring well in the
// future unless the caller overrides it.
func testSession(token string, expiry time.Time) model.Session {
	return model.Session{
		Token:  token,
		Data:   []byte("gob-encoded-data"),
		Expiry: expiry.UTC().Truncate(time.Second),
	}
}

// TestSessionUpsertAndByToken covers the round trip a fresh login makes: a
// session written by Upsert comes back identical through ByToken.
func TestSessionUpsertAndByToken(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	session := testSession("the-token", time.Now().Add(time.Hour))

	if err := db.Sessions().Upsert(t.Context(), session); err != nil {
		t.Fatalf("Upsert returned %v", err)
	}

	got, err := db.Sessions().ByToken(t.Context(), session.Token)
	if err != nil {
		t.Fatalf("ByToken returned %v", err)
	}

	if diff := cmp.Diff(session, got); diff != "" {
		t.Errorf("the session differs (-want +got):\n%s", diff)
	}
}

// TestSessionUpsertReplacesAnExistingRow pins RenewToken's use case: writing
// again with the same token updates the row in place rather than conflicting.
func TestSessionUpsertReplacesAnExistingRow(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	const token = "the-token"

	if err := db.Sessions().Upsert(t.Context(), testSession(token, time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("the first Upsert returned %v", err)
	}

	renewed := testSession(token, time.Now().Add(2*time.Hour))
	renewed.Data = []byte("renewed-data")

	if err := db.Sessions().Upsert(t.Context(), renewed); err != nil {
		t.Fatalf("the second Upsert returned %v", err)
	}

	got, err := db.Sessions().ByToken(t.Context(), token)
	if err != nil {
		t.Fatalf("ByToken returned %v", err)
	}

	if diff := cmp.Diff(renewed, got); diff != "" {
		t.Errorf("the session differs (-want +got):\n%s", diff)
	}
}

// TestSessionByTokenReportsMissing pins ErrNotFound rather than a zero
// session, both for a token never written and for one that has expired — an
// expired row is no session between two sweeps, not a valid one.
func TestSessionByTokenReportsMissing(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	if err := db.Sessions().Upsert(t.Context(), testSession("expired", time.Now().Add(-time.Minute))); err != nil {
		t.Fatalf("Upsert returned %v", err)
	}

	tests := map[string]string{
		"never written": "no-such-token",
		"expired":       "expired",
	}

	for name, token := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := db.Sessions().ByToken(t.Context(), token); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("ByToken returned %v, want ErrNotFound", err)
			}
		})
	}
}

// TestSessionDelete pins logout: the row is gone afterwards, and deleting a
// token that was never there is not an error, since scs calls this on every
// logout regardless of whether the session it names still exists.
func TestSessionDelete(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	session := testSession("the-token", time.Now().Add(time.Hour))
	if err := db.Sessions().Upsert(t.Context(), session); err != nil {
		t.Fatalf("Upsert returned %v", err)
	}

	if err := db.Sessions().Delete(t.Context(), session.Token); err != nil {
		t.Fatalf("Delete returned %v", err)
	}

	if _, err := db.Sessions().ByToken(t.Context(), session.Token); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after Delete, ByToken returned %v, want ErrNotFound", err)
	}

	if err := db.Sessions().Delete(t.Context(), "never-written"); err != nil {
		t.Errorf("Delete on an absent token returned %v, want nil", err)
	}
}

// TestSessionDeleteExpired is the sweep: an expired row is removed and an
// unexpired one is left alone.
func TestSessionDeleteExpired(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	expired := testSession("expired", time.Now().Add(-time.Minute))
	current := testSession("current", time.Now().Add(time.Hour))

	if err := db.Sessions().Upsert(t.Context(), expired); err != nil {
		t.Fatalf("upserting the expired session: %v", err)
	}

	if err := db.Sessions().Upsert(t.Context(), current); err != nil {
		t.Fatalf("upserting the current session: %v", err)
	}

	if err := db.Sessions().DeleteExpired(t.Context()); err != nil {
		t.Fatalf("DeleteExpired returned %v", err)
	}

	var count int
	if err := db.q.QueryRowContext(t.Context(), `SELECT count(*) FROM sessions WHERE token = ?`, expired.Token).Scan(&count); err != nil {
		t.Fatalf("counting the expired row: %v", err)
	}

	if count != 0 {
		t.Errorf("the expired session row is still there after DeleteExpired")
	}

	if _, err := db.Sessions().ByToken(t.Context(), current.Token); err != nil {
		t.Errorf("the current session was swept too: ByToken returned %v", err)
	}
}
