package httpapi

import (
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// redacted stands in for a value left out of the log. It is a marker rather
// than an omission so that the line still shows the client sent something: "no
// Authorization header" and "an Authorization header we did not print" are
// different findings about a client whose behaviour is what is being read off
// this log.
const redacted = "[REDACTED]"

// sensitiveWords make a value a credential as far as this log is concerned: a
// query parameter or a header whose name contains one of them is logged with
// its value replaced.
//
// `api_key` and `Authorization` are the two the API documents, and matching a
// name against a word list rather than against those two is the whole point.
// The client this log exists to observe is closed source, so it may carry a
// credential under a name nobody here predicted — `X-Auth-Token` is as likely
// as either — and a name that cannot be predicted cannot be enumerated. The
// cost of the rule being too eager is a redacted value in a debug log; the
// cost of it not being eager enough is a live credential in one.
var sensitiveWords = []string{
	"key", "token", "pass", "pwd", "secret", "auth", "credential", "session", "signature", "jwt",
}

// sensitiveHeaders are the headers the word list does not catch, because
// nothing in their name says what they carry.
var sensitiveHeaders = []string{"Cookie"}

// urlValuedHeaders are the headers whose value is a URL rather than a bare
// credential, so the redaction they need is [redactQuery]'s, applied to
// their own query string, not [redacted]'s. Enumerated rather than detected
// by shape: parsing every header as a URL would mangle an ordinary header
// that merely contains a "?", where the credential this guards against —
// api_key travels in the query string, per "Authentication" — only ever
// arrives by a client copying the URL it was given, and Referer is the
// header that carries one.
var urlValuedHeaders = []string{"Referer"}

// logRequests writes one line per request: what was asked for, and what it was
// answered with.
//
// This is endpoint discovery rather than an access log, which is what decides
// where it sits and what it prints. It is the router's first middleware, and
// chi runs its middleware chain before it matches a route, so a request that
// matches none is logged too: those are answered 404, and a 404 here is an
// endpoint the app wants and this server does not serve yet. The whole point
// being to capture a real device, the log has to be complete enough to plan
// from and carry none of the credentials that device sends.
//
// It logs at Info and not Debug on purpose. TRAVELMAP_DEBUG_LOG_REQUESTS is
// the switch; having to raise TRAVELMAP_LOG_LEVEL as well would be a second
// one, and a capture session that produced no log is a session to run again.
// [config.Load] raises a level that would swallow these lines for the same
// reason.
//
// Request bodies are never logged. POST /api/v1/auth/login carries a password
// in its body, and a body is the one part of a request that cannot be redacted
// without parsing it as whatever the endpoint takes.
func (a *api) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		// Deferred rather than written after the call, so that a request
		// unwinding on a panic the recovery deliberately re-raises still
		// leaves the line that says which request it was.
		defer func() {
			a.logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"query", redactQuery(r.URL.Query()),
				"status", statusOf(rec),
				"bytes", rec.BytesWritten(),
				"duration", time.Since(start),
				slog.Group("header", headerAttrs(r.Header)...),
			)
		}()

		next.ServeHTTP(rec, r)
	})
}

// statusOf reports the status a response went out with. A handler that wrote
// nothing at all leaves the recorder at zero, where net/http sends 200.
func statusOf(w middleware.WrapResponseWriter) int {
	if status := w.Status(); status != 0 {
		return status
	}

	return http.StatusOK
}

// redactQuery renders a query string for the log, with the value of every
// parameter that could be a credential replaced.
//
// The names are kept whatever they are: which parameters a client sends is
// exactly what is being discovered here, and only a value can be secret. The
// result is sorted by name so that the same request logs the same line twice,
// and the values are the decoded ones, because a log is read rather than
// re-parsed.
func redactQuery(query url.Values) string {
	var b strings.Builder

	for _, name := range slices.Sorted(maps.Keys(query)) {
		for _, value := range query[name] {
			if b.Len() > 0 {
				b.WriteByte('&')
			}

			b.WriteString(name)
			b.WriteByte('=')
			b.WriteString(queryValue(name, value))
		}
	}

	return b.String()
}

// queryValue returns what a query parameter is logged as.
func queryValue(name, value string) string {
	if sensitiveName(name) {
		return redacted
	}

	return value
}

// sensitiveName reports whether the value carried under name is to be treated
// as a credential. Query parameters and headers are judged by the same rule:
// a credential under an unexpected name is as likely in one as in the other.
func sensitiveName(name string) bool {
	lower := strings.ToLower(name)

	for _, word := range sensitiveWords {
		if strings.Contains(lower, word) {
			return true
		}
	}

	return false
}

// headerAttrs renders the request headers for the log, sorted by name and with
// the credential-carrying ones replaced.
//
// Every header is logged rather than a chosen few. Which headers a client
// sends is part of what this log is for, and a shortlist of the interesting
// ones could only be written by someone who already knew what the client does.
func headerAttrs(header http.Header) []any {
	attrs := make([]any, 0, len(header))

	for _, name := range slices.Sorted(maps.Keys(header)) {
		attrs = append(attrs, slog.String(name, headerValue(name, header[name])))
	}

	return attrs
}

// headerValue joins the values a header arrived with, unless the header is one
// whose value is a credential, or one whose value is a URL that could carry
// one in its query string.
func headerValue(name string, values []string) string {
	if sensitiveName(name) || slices.Contains(sensitiveHeaders, http.CanonicalHeaderKey(name)) {
		return redacted
	}

	if slices.Contains(urlValuedHeaders, http.CanonicalHeaderKey(name)) {
		values = redactURLs(values)
	}

	return strings.Join(values, ", ")
}

// redactURLs returns values with the query string of each redacted the same
// way [redactQuery] redacts the request's own. A value that does not parse as
// a URL, or carries no query string, is returned unchanged.
func redactURLs(values []string) []string {
	out := make([]string, len(values))

	for i, value := range values {
		out[i] = redactURL(value)
	}

	return out
}

// redactURL is [redactURLs] for one value.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.RawQuery == "" {
		return raw
	}

	u.RawQuery = redactQuery(u.Query())

	return u.String()
}
