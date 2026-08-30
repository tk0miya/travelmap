package foursquare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The fixed parameters of every check-in request.
const (
	// apiVersion is the required `v` parameter: the date whose documented
	// response shape this client was written against. It is pinned rather
	// than moved to today's date on every request, because that is what makes
	// the response shape stop changing underneath the code that reads it. A
	// pinned version going out of support arrives as meta.errorType
	// "deprecated" on an otherwise successful response, which is why
	// [Client.Checkins] logs that one loudly.
	apiVersion = "20260824"

	// pageLimit is the `limit` parameter, at its documented maximum: a
	// fortnight of one account's check-ins is expected to fit in a single
	// page, so the fetch that finds nothing new costs one request.
	pageLimit = 250
)

// requestTimeout bounds one request, from dialling to the last byte of the
// response body. A page-walk's own bound is the context it is given: a
// per-request timeout is what keeps one stalled connection from holding the
// walk forever, and a walk of many pages is legitimately allowed to take
// longer than one request may.
const requestTimeout = 30 * time.Second

// maxResponseBody bounds how much of a response is read. A full page of 250
// check-ins with a venue each is a few hundred kilobytes; eight megabytes is
// far more than that and still small enough that a body which never ends is
// refused rather than read into memory forever. A response cut off at the
// limit fails to parse as JSON, which is the intended outcome: a truncated
// page is not a short page.
const maxResponseBody = 8 << 20

// ErrNoProgress reports a page-walk that could not advance: a full page
// reaching no further back than the one before it, so the next request would
// return the same check-ins forever.
//
// This is the shape an `offset` that is accepted and ignored produces, and it
// is deliberately an error rather than an end of the walk — a walk that
// stopped quietly here would look like a successful sync of one page, every
// time, forever.
var ErrNoProgress = errors.New("foursquare: the check-in page walk cannot advance")

// APIError is a request Foursquare refused.
//
// Both the HTTP status and the `meta` block are kept: Foursquare's own
// wording is that it "attempts to use appropriate HTTP status codes", and its
// mapping is not the one a client would guess — a 403 is both a passing
// refusal and a revoked authorisation, which are not the same thing. So
// ErrorType is what a caller deciding how to react should read. RequestID is
// what a support question has to quote.
type APIError struct {
	StatusCode  int
	Code        int
	ErrorType   string
	ErrorDetail string
	RequestID   string
}

// Error implements [error].
func (e *APIError) Error() string {
	return fmt.Sprintf("foursquare: the API answered HTTP %d (meta.code %d, errorType %q, %q, requestId %q)",
		e.StatusCode, e.Code, e.ErrorType, e.ErrorDetail, e.RequestID)
}

// Client calls the Foursquare v2 API on behalf of one access token at a time:
// the token is a per-account credential and is passed per call, where the
// endpoint and the HTTP client are the server's own and are held here.
type Client struct {
	baseURL string
	http    *http.Client
	logger  *slog.Logger
}

// NewClient returns a client for the API at baseURL — the real one, or the
// address of a test server standing in for it, which is what
// foursquare.api_url is for. The default is the configuration's to
// hold, so nothing here supplies one.
//
// logger carries the one thing this client reports without being asked: the
// deprecation notice that arrives on an otherwise successful response.
func NewClient(baseURL string, logger *slog.Logger) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{Timeout: requestTimeout},
		logger:  logger,
	}
}

// CheckinsQuery is the position of one check-in request in the walk.
type CheckinsQuery struct {
	// Offset is how many of the newest check-ins to skip, sent as offset. It
	// is 0 on a walk's first request, which asks for the newest page.
	Offset int
}

// Checkins fetches one page of the token holder's own check-ins, newest
// first. It returns the page's items in the order the server sent them.
func (c *Client) Checkins(ctx context.Context, token string, query CheckinsQuery) ([]Checkin, error) {
	values := url.Values{}
	values.Set("v", apiVersion)
	values.Set("limit", strconv.Itoa(pageLimit))

	if query.Offset > 0 {
		values.Set("offset", strconv.Itoa(query.Offset))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/v2/users/self/checkins?"+values.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("foursquare: building the check-in request: %w", err)
	}

	// The token goes in a header rather than in the oauth_token query
	// parameter the endpoint's own reference names, so that it stays out of
	// proxy logs, error messages and anything else that echoes a request
	// line.
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("foursquare: fetching check-ins: %w", err)
	}

	defer func() {
		// Drained before closing, so that the connection can be reused for
		// the next page rather than dropped mid-body.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("foursquare: reading the check-in response: %w", err)
	}

	return c.decodeCheckins(resp.StatusCode, body)
}

// checkinsResponse is the envelope every v2 response arrives in.
type checkinsResponse struct {
	Meta     responseMeta `json:"meta"`
	Response struct {
		Checkins struct {
			// Items are decoded twice over: once as raw JSON, so that each
			// check-in keeps the bytes it arrived as, and once into the
			// struct whose fields become columns.
			Items []json.RawMessage `json:"items"`
		} `json:"checkins"`
	} `json:"response"`
}

// responseMeta is the `meta` block, which repeats the status and carries the
// error detail. It is read rather than trusting the HTTP status alone: the
// documentation only says appropriate statuses are attempted, and it names
// one case that arrives on a 200 — errorType "deprecated".
type responseMeta struct {
	Code         int    `json:"code"`
	ErrorType    string `json:"errorType"`
	ErrorDetail  string `json:"errorDetail"`
	ErrorMessage string `json:"errorMessage"`
	RequestID    string `json:"requestId"`
}

// deprecatedErrorType is the meta.errorType that arrives on an otherwise
// successful response when the pinned `v`, or a field read out of it, is on
// its way out. It is the only warning this client will ever get before a
// pinned version stops answering.
const deprecatedErrorType = "deprecated"

// decodeCheckins turns one response into the page it carries, or into the
// error it reports.
func (c *Client) decodeCheckins(status int, body []byte) ([]Checkin, error) {
	var decoded checkinsResponse

	// Decoded before the status is judged: a refusal carries its reason in
	// the same envelope, and errorType is what a caller branches on. A body
	// that does not parse at all is reported as the status alone, since
	// there is nothing else to say about it.
	if err := json.Unmarshal(body, &decoded); err != nil {
		if status != http.StatusOK {
			return nil, &APIError{StatusCode: status, Code: 0, ErrorType: "", ErrorDetail: "", RequestID: ""}
		}

		return nil, fmt.Errorf("foursquare: parsing the check-in response: %w", err)
	}

	if status != http.StatusOK {
		return nil, &APIError{
			StatusCode:  status,
			Code:        decoded.Meta.Code,
			ErrorType:   decoded.Meta.ErrorType,
			ErrorDetail: strings.TrimSpace(decoded.Meta.ErrorDetail + " " + decoded.Meta.ErrorMessage),
			RequestID:   decoded.Meta.RequestID,
		}
	}

	if decoded.Meta.ErrorType == deprecatedErrorType {
		c.logger.Warn("the Foursquare API reports this client's pinned version as deprecated",
			"version", apiVersion,
			"detail", decoded.Meta.ErrorDetail,
			"message", decoded.Meta.ErrorMessage,
			"requestId", decoded.Meta.RequestID,
		)
	}

	checkins := make([]Checkin, 0, len(decoded.Response.Checkins.Items))

	for _, item := range decoded.Response.Checkins.Items {
		var checkin Checkin

		if err := json.Unmarshal(item, &checkin); err != nil {
			return nil, fmt.Errorf("foursquare: parsing a fetched check-in: %w", err)
		}

		checkin.Raw = item

		checkins = append(checkins, checkin)
	}

	return checkins, nil
}

// EachCheckinPage walks the token holder's check-ins created at or after
// after, calling fn with each page's check-ins that fall inside that window,
// as the page arrives. A page is handed over before the next one is
// requested, so a walk of a long backfill holds one page in memory rather
// than all of them, and an error from fn stops the walk and is returned as it
// stands.
//
// The walk pages by offset over check-ins the endpoint answers newest first,
// which is what lets it stop before reading a whole history. The window's
// lower bound is applied to each page here rather than asked of the server.
//
// A page shorter than the request limit is the end of the data, and a page
// reaching past the window's start is the end of the window. A full page that
// reaches no further back than the one before it returns [ErrNoProgress]:
// that is a third condition, and treating it as an end is how an offset that
// is silently ignored would look like a successful sync of the same newest
// page, forever.
//
// An offset walks a list that an arriving check-in shifts underneath it, so a
// page boundary can repeat a check-in or step over one. Neither is prevented
// here; what absorbs them is where check-ins are written and how the window
// is chosen.
func (c *Client) EachCheckinPage(ctx context.Context, token string, after time.Time, fn func([]Checkin) error) error {
	var (
		query    CheckinsQuery
		previous time.Time
	)

	for {
		page, err := c.Checkins(ctx, token, query)
		if err != nil {
			return err
		}

		if len(page) == 0 {
			return nil
		}

		oldest := oldestCreatedAt(page)

		if within := inWindow(page, after); len(within) > 0 {
			if err := fn(within); err != nil {
				return err
			}
		}

		if len(page) < pageLimit || oldest.Before(after) {
			return nil
		}

		// Only a full page that is being asked to continue can have failed to
		// advance, and only against a page before it — a walk's first request
		// has none.
		if query.Offset > 0 && !oldest.Before(previous) {
			return fmt.Errorf("%w: a full page reaching back to %s did not pass the previous page's %s",
				ErrNoProgress, oldest.Format(time.RFC3339), previous.Format(time.RFC3339))
		}

		previous = oldest
		query.Offset += pageLimit
	}
}

// inWindow is the page's check-ins created at or after after, in the order
// the page carried them.
func inWindow(page []Checkin, after time.Time) []Checkin {
	if after.IsZero() {
		return page
	}

	start := after.Unix()
	within := make([]Checkin, 0, len(page))

	for _, checkin := range page {
		if checkin.CreatedAt >= start {
			within = append(within, checkin)
		}
	}

	return within
}

// oldestCreatedAt is the page's minimum createdAt.
//
// It is the page's minimum rather than its last element on purpose: a server
// returning the right check-ins in an order it never promised still walks
// correctly this way.
func oldestCreatedAt(page []Checkin) time.Time {
	oldest := page[0].CreatedAt

	for _, checkin := range page[1:] {
		if checkin.CreatedAt < oldest {
			oldest = checkin.CreatedAt
		}
	}

	return time.Unix(oldest, 0).UTC()
}
