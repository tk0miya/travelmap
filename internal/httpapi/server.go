package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const (
	// readHeaderTimeout bounds how long a connection may take to send its
	// request headers, which is what keeps a Slowloris client from holding a
	// connection open indefinitely.
	readHeaderTimeout = 10 * time.Second

	// shutdownTimeout bounds the wait for in-flight requests once shutdown has
	// started. This server's requests are short, so a request still running
	// after this is stuck rather than slow.
	shutdownTimeout = 10 * time.Second
)

// Serve listens on addr and serves h until ctx is cancelled, then stops
// accepting connections and gives the in-flight requests up to shutdownTimeout
// to finish.
//
// It returns nil for an ordinary shutdown, so a cancelled context is not an
// error at the call site.
func Serve(ctx context.Context, addr string, h http.Handler, logger *slog.Logger) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	return serve(ctx, ln, h, logger)
}

// serve is Serve with the listener already open, which is what lets a test
// take an ephemeral port and still know which one it got.
func serve(ctx context.Context, ln net.Listener, h http.Handler, logger *slog.Logger) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	// Buffered, so the goroutine can exit even if nobody reads the result.
	srvErr := make(chan error, 1)

	go func() {
		logger.Info("listening", "addr", ln.Addr().String())
		srvErr <- srv.Serve(ln)
	}()

	select {
	case err := <-srvErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down", "timeout", shutdownTimeout)

	// context.WithoutCancel, because ctx is already cancelled: the shutdown
	// deadline is what bounds the wait, not the signal that started it.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down: %w", err)
	}

	<-srvErr

	return nil
}
