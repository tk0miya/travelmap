package httpapi

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Options carries what the HTTP surface needs from its caller. Later steps add
// the store and the ingest layer here; cmd/travelmap is the only place that
// fills it in.
type Options struct {
	// Logger receives everything the HTTP layer logs. Required.
	Logger *slog.Logger
}

// api holds the dependencies shared by the handlers. Handlers are methods on
// it rather than free functions, so a new dependency is added in one place
// instead of being threaded through every signature.
type api struct {
	logger *slog.Logger
}

// New builds the server's HTTP handler.
func New(opts Options) http.Handler {
	a := &api{logger: opts.Logger}

	r := chi.NewRouter()

	// The recovery covers the whole server, not just the API: it is about a
	// bug not taking the process down. The Dawarich headers are compatibility,
	// so they stay on the group they describe.
	r.Use(a.recoverer)

	r.NotFound(a.notFound)
	r.MethodNotAllowed(a.methodNotAllowed(r))

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(dawarichHeaders)

		get(r, "/health", a.health)
	})

	return r
}

// get registers h for both GET and HEAD.
//
// chi answers 405 to the HEAD of a route declared with Get, where upstream
// answers it like any GET; a client that probes with HEAD reads the 405 as
// "nothing to fetch". Registering both here means no later handler has to
// remember it. See "GET /api/v1/points must answer HTTP HEAD" in TODO.md.
// net/http discards the body of a HEAD response, so h needs no branch.
func get(r chi.Router, pattern string, h http.HandlerFunc) {
	r.Get(pattern, h)
	r.Head(pattern, h)
}

// notFound answers a request for a route this server does not serve.
//
// It has to stay a 404 with no usable body: Dawarich has no version
// negotiation, so clients probe an endpoint and read 404 as "this server does
// not have the feature". Answering 200 with an empty array instead tells the
// client the feature exists and it will show UI that then misbehaves.
func (a *api) notFound(w http.ResponseWriter, r *http.Request) {
	a.writeError(w, r, http.StatusNotFound, "not found")
}

// methodNotAllowed answers a known route addressed with a method it does not
// serve. chi registers methods explicitly, which is what makes this reachable.
//
// It takes the router because a 405 owes the client an Allow header (RFC 9110
// makes it a MUST), and chi's own list of allowed methods is unexported: a
// replacement handler cannot read it, so the methods are recovered by asking
// the router which ones would match this path. Those extra lookups only ever
// happen on a request that is already failing.
func (a *api) methodNotAllowed(routes chi.Routes) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if allowed := allowedMethods(routes, r.URL.Path); len(allowed) > 0 {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
		}

		a.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// probedMethods are the methods an Allow header is built out of: every method
// this server registers anywhere, so that the answer is complete.
var probedMethods = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
}

// allowedMethods reports which of probedMethods the router would route to a
// handler for path.
func allowedMethods(routes chi.Routes, path string) []string {
	allowed := make([]string, 0, len(probedMethods))

	for _, method := range probedMethods {
		if routes.Match(chi.NewRouteContext(), method, path) {
			allowed = append(allowed, method)
		}
	}

	return allowed
}
