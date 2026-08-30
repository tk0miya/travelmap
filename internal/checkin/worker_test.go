package checkin_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/checkin"
	"github.com/tk0miya/travelmap/internal/foursquare"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// mutableFetchServer is [fetchServer] whose response can be changed between
// requests, standing in for a check-in that arrives while the worker below is
// stopped.
type mutableFetchServer struct {
	mu       sync.Mutex
	checkins []string
}

// set replaces what the next request sees.
func (s *mutableFetchServer) set(checkins ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.checkins = checkins
}

// client starts the server, closed on test cleanup, and returns a client
// pointed at it.
func (s *mutableFetchServer) client(t *testing.T) *foursquare.Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		checkins := s.checkins
		s.mu.Unlock()

		body := `{"meta":{"code":200},"response":{"checkins":{"count":` +
			strconv.Itoa(len(checkins)) + `,"items":[`

		for i, c := range checkins {
			if i > 0 {
				body += ","
			}

			body += c
		}

		body += `]}}}`

		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return foursquare.NewClient(server.URL, slog.New(slog.DiscardHandler))
}

// countingFetcher wraps a [checkin.Fetcher], counting a call only once it has
// returned with no error — which for the real client happens only after its
// callback, and so the write that callback makes, has already completed. A
// test can therefore wait on the count instead of polling the store: there is
// no [store.CheckinRepository] read to poll with that would not itself be a
// write racing the one under test (see probeCheckin's own doc comment).
type countingFetcher struct {
	inner checkin.Fetcher
	calls atomic.Int32
}

func (f *countingFetcher) EachCheckinPage(ctx context.Context, token string, after time.Time, fn func([]foursquare.Checkin) error) error {
	err := f.inner.EachCheckinPage(ctx, token, after, fn)
	if err == nil {
		f.calls.Add(1)
	}

	return err
}

// waitForCall blocks until fetcher has completed at least one call, or fails
// the test after timeout.
func waitForCall(t *testing.T, fetcher *countingFetcher, timeout time.Duration) {
	t.Helper()

	deadline := time.After(timeout)
	for fetcher.calls.Load() < 1 {
		select {
		case <-deadline:
			t.Fatal("the worker did not complete a sync call before the deadline")
		case <-time.After(time.Millisecond):
		}
	}
}

// TestRunPeriodicSyncTicksAndStops verifies the worker end to end: a linked
// account's check-in is collected on a tick, and cancelling the context stops
// the goroutine rather than leaving it running.
func TestRunPeriodicSyncTicksAndStops(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	linkedUser(t, st)

	server := &mutableFetchServer{}
	server.set(checkinJSON("aaa111", time.Now().UTC().Add(-time.Hour)))
	fetcher := &countingFetcher{inner: server.client(t)}

	ctx, cancel := context.WithCancel(t.Context())
	logger := slog.New(slog.DiscardHandler)

	done := make(chan struct{})
	go func() {
		checkin.RunPeriodicSync(ctx, st, fetcher, time.Millisecond, testLookback, logger)
		close(done)
	}()

	waitForCall(t, fetcher, time.Second)

	if got := probeCheckin(t, st, "aaa111"); got.Source != checkin.SourceSync {
		t.Errorf("aaa111 has source %q, want %q", got.Source, checkin.SourceSync)
	}

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunPeriodicSync kept running after its context was cancelled")
	}
}

// TestRunPeriodicSyncRestartLosesNoTick pins the one thing worth testing about
// the worker's own restart, as opposed to Sync's recomputed window (already
// covered in sync_test.go): a check-in that arrives while the worker is
// stopped is still collected by the very first tick after it starts again,
// because there is no cursor for a stop to leave stale.
func TestRunPeriodicSyncRestartLosesNoTick(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	linkedUser(t, st)

	server := &mutableFetchServer{}
	server.set(checkinJSON("aaa111", time.Now().UTC().Add(-time.Hour)))
	client := server.client(t)

	logger := slog.New(slog.DiscardHandler)

	ctx, cancel := context.WithCancel(t.Context())
	fetcher := &countingFetcher{inner: client}

	done := make(chan struct{})
	go func() {
		checkin.RunPeriodicSync(ctx, st, fetcher, time.Millisecond, testLookback, logger)
		close(done)
	}()

	waitForCall(t, fetcher, time.Second)

	// Stop the worker, exactly like a server shutting down.
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunPeriodicSync kept running after its context was cancelled")
	}

	// A second check-in arrives while nothing is running to collect it.
	server.set(
		checkinJSON("aaa111", time.Now().UTC().Add(-time.Hour)),
		checkinJSON("bbb222", time.Now().UTC().Add(-30*time.Minute)),
	)

	// Restart, standing in for the server starting back up.
	ctx2, cancel2 := context.WithCancel(t.Context())
	defer cancel2()

	restarted := &countingFetcher{inner: client}

	go checkin.RunPeriodicSync(ctx2, st, restarted, time.Millisecond, testLookback, logger)

	waitForCall(t, restarted, time.Second)

	if got := probeCheckin(t, st, "bbb222"); got.Source != checkin.SourceSync {
		t.Errorf("bbb222 has source %q, want %q", got.Source, checkin.SourceSync)
	}
}
