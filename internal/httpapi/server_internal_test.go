package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestServeShutsDownGracefully pins that cancelling the context stops the
// server without cutting off a request that is already running: an in-flight
// point upload must not be lost because the process was asked to stop.
func TestServeShutsDownGracefully(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release

		if _, err := io.WriteString(w, "done"); err != nil {
			t.Errorf("writing the response: %v", err)
		}
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())

	serveErr := make(chan error, 1)

	go func() {
		serveErr <- serve(ctx, ln, handler, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	respErr := make(chan error, 1)
	respBody := make(chan string, 1)

	go func() {
		req, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodGet, "http://"+ln.Addr().String(), nil)
		if err != nil {
			respErr <- err

			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			respErr <- err

			return
		}

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			respErr <- err

			return
		}

		respBody <- string(body)
	}()

	<-started
	cancel()
	waitUntilRefused(t, ln.Addr().String())

	// Closing the listener is the first thing Shutdown does, so the shutdown
	// is now under way with the handler still running: it must wait for the
	// handler rather than close the connection under it.
	close(release)

	select {
	case err := <-respErr:
		t.Fatalf("the in-flight request failed: %v", err)
	case body := <-respBody:
		if body != "done" {
			t.Errorf("body = %q, want %q", body, "done")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the in-flight request did not finish")
	}

	select {
	case err := <-serveErr:
		if err != nil {
			t.Errorf("serve returned %v, want nil for an ordinary shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after the context was cancelled")
	}
}

// waitUntilRefused blocks until nothing accepts connections on addr any more,
// which is how a test observes that the shutdown has started.
func waitUntilRefused(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return
		}

		if err := conn.Close(); err != nil {
			t.Fatalf("closing the probe connection: %v", err)
		}
	}

	t.Fatal("the listener was still accepting after the context was cancelled")
}

// TestServeReportsAListenFailure pins that a port already in use is reported
// rather than swallowed, since the process would otherwise look healthy while
// serving nothing.
func TestServeReportsAListenFailure(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	defer ln.Close()

	err = Serve(t.Context(), ln.Addr().String(), http.NotFoundHandler(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("Serve returned nil for an address already in use")
	}
}
