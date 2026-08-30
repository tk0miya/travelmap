package track_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/storetest"
	"github.com/tk0miya/travelmap/internal/track"
)

// discardLogger is what every test in this file passes RunWorker: none of
// them wants to see what the worker logs.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitForTracks polls until userID has at least one track, or fails the test
// after a second — long enough for a goroutine to actually be scheduled, far
// short of [track.RunWorker]'s own poll interval, which this test exists to
// show is not what made the rebuild happen.
func waitForTracks(t *testing.T, st store.Store, userID int64) []model.Track {
	t.Helper()

	deadline := time.Now().Add(time.Second)

	for time.Now().Before(deadline) {
		tracks, _, err := st.Tracks().List(t.Context(), userID, nil, nil, 1, 10)
		if err != nil {
			t.Fatalf("listing tracks: %v", err)
		}

		if len(tracks) > 0 {
			return tracks
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("no track appeared within a second")

	return nil
}

// TestRunWorkerDrainsPendingRequestsOnStart pins that RunWorker rebuilds a
// pending request without waiting for its first tick — the "catch up on
// anything left pending" half of its own doc comment, and the reason this
// test does not need to wait out the worker's own poll interval.
func TestRunWorkerDrainsPendingRequestsOnStart(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	start := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

	seedPoints(t, st, []model.Point{
		{UserID: 1, Timestamp: start, Latitude: 1, Longitude: 1},
		{UserID: 1, Timestamp: start.Add(time.Minute), Latitude: 2, Longitude: 2},
	})

	if err := st.Tracks().Enqueue(t.Context(), 1); err != nil {
		t.Fatalf("enqueuing: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go track.RunWorker(ctx, st, 30*time.Minute, discardLogger())

	waitForTracks(t, st, 1)
}

// TestRunWorkerLeavesAFailedRequestPendingForTheNextTick pins that a
// rebuild failing rolls back its own claim along with it — [store.Store.Tx]'s
// join behaviour, exercised here through the worker — so the request is not
// silently lost, only delayed until the underlying failure clears.
func TestRunWorkerLeavesAFailedRequestPendingForTheNextTick(t *testing.T) {
	t.Parallel()

	st := storetest.UnavailableTracks(t, testUser())

	if err := st.Tracks().Enqueue(t.Context(), 1); err != nil {
		t.Fatalf("enqueuing: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go track.RunWorker(ctx, st, 30*time.Minute, discardLogger())

	// Long enough for the worker's own startup drain to run and fail; short
	// of its next poll, which would otherwise retry and could mask a claim
	// that was not actually rolled back.
	time.Sleep(100 * time.Millisecond)

	userID, ok, err := st.Tracks().NextPending(t.Context())
	if err != nil {
		t.Fatalf("NextPending returned %v", err)
	}

	if !ok || userID != 1 {
		t.Errorf("NextPending = (%d, %v), want (1, true): a failed rebuild must leave its request claimable again", userID, ok)
	}
}

// TestRunWorkerStopsOnContextCancellation pins that RunWorker returns once
// its context is done, rather than leaking the goroutine cmd/travelmap/serve.go
// starts it in.
func TestRunWorkerStopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})

	go func() {
		track.RunWorker(ctx, st, 30*time.Minute, discardLogger())
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunWorker did not return within a second of its context being cancelled")
	}
}
