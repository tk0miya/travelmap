package main

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	// Opens a connection of its own to read a row the store's own interfaces
	// do not expose: ByToken filters expired rows the same way whether they
	// are still there or already swept, so telling the two apart needs SQL
	// that reads the table directly.
	_ "modernc.org/sqlite"

	"github.com/tk0miya/travelmap/internal/model"
)

// countSessionRow reports how many rows the sessions table holds for token,
// regardless of expiry.
func countSessionRow(t *testing.T, path, token string) int {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening a raw connection: %v", err)
	}

	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing the raw connection: %v", err)
		}
	}()

	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sessions WHERE token = ?`, token).Scan(&count); err != nil {
		t.Fatalf("counting the %q row: %v", token, err)
	}

	return count
}

// TestSweepExpiredSessions verifies the worker end to end: an expired row is
// removed, an unexpired one is left alone, and stopping its context stops the
// goroutine rather than leaving it ticking.
func TestSweepExpiredSessions(t *testing.T) {
	t.Parallel()

	env, path := tempDatabase(t)

	if err := run([]string{"migrate"}, env, noStdin(), new(bytes.Buffer), new(bytes.Buffer)); err != nil {
		t.Fatalf("migrate returned %v", err)
	}

	ctx := t.Context()

	db, _, err := openDatabase(ctx, env)
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	defer closeDatabase(db)

	expired := model.Session{Token: "expired", Data: []byte("{}"), Expiry: time.Now().Add(-time.Minute)}
	current := model.Session{Token: "current", Data: []byte("{}"), Expiry: time.Now().Add(time.Hour)}

	if err := db.Sessions().Upsert(ctx, expired); err != nil {
		t.Fatalf("upserting the expired session: %v", err)
	}

	if err := db.Sessions().Upsert(ctx, current); err != nil {
		t.Fatalf("upserting the current session: %v", err)
	}

	sweepCtx, cancel := context.WithCancel(ctx)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	done := make(chan struct{})
	go func() {
		sweepExpiredSessions(sweepCtx, db, time.Millisecond, logger)
		close(done)
	}()

	deadline := time.After(time.Second)

	for countSessionRow(t, path, expired.Token) != 0 {
		select {
		case <-deadline:
			t.Fatal("the expired session row is still there after waiting for a sweep tick")
		case <-time.After(time.Millisecond):
		}
	}

	if got := countSessionRow(t, path, current.Token); got != 1 {
		t.Errorf("the current session row count is %d, want 1: the sweep removed a row it should not have", got)
	}

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sweepExpiredSessions kept running after its context was cancelled")
	}
}
