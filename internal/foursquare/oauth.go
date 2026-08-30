package foursquare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
)

// authenticateURL is Foursquare's v2 OAuth authenticate endpoint. Unlike
// DefaultTokenURL and DefaultAPIBaseURL below, nothing here ever sends it a
// request — AuthenticateURL only builds a redirect string out of it — so no
// caller, test or otherwise, has a reason to point it anywhere else. It is
// unexported and used directly rather than taken as a parameter or exported
// as a "Default" the way the other two are, since there is no non-default
// value it would ever be a default of.
const authenticateURL = "https://foursquare.com/oauth2/authenticate"

// The two v2 OAuth endpoints ExchangeCode and GetSelfUserID make a real HTTP
// request to, so a caller wanting a fake server for testing points them at
// one explicitly instead.
const (
	DefaultTokenURL   = "https://foursquare.com/oauth2/access_token" //nolint:gosec // a URL, not a credential
	DefaultAPIBaseURL = "https://api.foursquare.com"
)

// AuthenticateURL returns the URL to send a browser to, to start the OAuth
// flow: clientID and redirectURL identify the Foursquare application and
// where it sends the browser back, and state is echoed back on that
// redirect. Foursquare's own reference documents neither state nor scope for
// this endpoint, so a caller verifies the echo rather than assuming it.
func AuthenticateURL(clientID, redirectURL, state string) string {
	v := url.Values{
		"client_id":     {clientID},
		"response_type": {"code"},
		"redirect_uri":  {redirectURL},
		"state":         {state},
	}

	return authenticateURL + "?" + v.Encode()
}

// TokenResponse is what ExchangeCode reads out of a token exchange.
type TokenResponse struct {
	// AccessToken is the only field the reference documents.
	AccessToken string

	// Fields is the JSON object's own key names, exactly as the response
	// sent them. Whether a refresh token or an expiry also comes back is
	// untested — the reference's silence is read as unsettled rather than
	// as an absence — so a caller logs this rather than assuming
	// access_token is all there is.
	Fields []string
}

// ExchangeCode redeems an OAuth authorization code for an access token at
// tokenURL.
func ExchangeCode(ctx context.Context, client *http.Client, tokenURL, clientID, clientSecret, redirectURL, code string) (TokenResponse, error) {
	v := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURL},
		"code":          {code},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL+"?"+v.Encode(), nil)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("foursquare: building the token exchange request: %w", err)
	}

	body, err := doRequest(client, req, "exchanging the OAuth code")
	if err != nil {
		return TokenResponse{}, err
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return TokenResponse{}, fmt.Errorf("foursquare: parsing the token exchange response: %w", err)
	}

	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return TokenResponse{}, fmt.Errorf("foursquare: parsing the token exchange response: %w", err)
	}

	if token.AccessToken == "" {
		return TokenResponse{}, errors.New("foursquare: the token exchange response carried no access_token")
	}

	return TokenResponse{AccessToken: token.AccessToken, Fields: fieldNames(fields)}, nil
}

// GetSelfUserID calls GET /v2/users/self at apiBaseURL with accessToken and
// returns the Swarm account's own id. It pins the same apiVersion the
// check-in client does, rather than a date of its own: one pin to raise
// deliberately instead of two to keep in step.
func GetSelfUserID(ctx context.Context, client *http.Client, apiBaseURL, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		apiBaseURL+"/v2/users/self?v="+apiVersion, nil)
	if err != nil {
		return "", fmt.Errorf("foursquare: building the users/self request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	body, err := doRequest(client, req, "calling users/self")
	if err != nil {
		return "", err
	}

	var parsed struct {
		Response struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("foursquare: parsing the users/self response: %w", err)
	}

	if parsed.Response.User.ID == "" {
		return "", errors.New("foursquare: the users/self response carried no user id")
	}

	return parsed.Response.User.ID, nil
}

// doRequest issues req and returns its body, treating any non-200 status as
// a failure — action names what was being attempted, for the error message.
func doRequest(client *http.Client, req *http.Request, action string) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("foursquare: %s: %w", action, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("foursquare: reading the response of %s: %w", action, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("foursquare: %s: %s: %s", action, resp.Status, body)
	}

	return body, nil
}

// fieldNames returns m's keys, sorted so a caller logging them gets the same
// line for the same response every time.
func fieldNames(m map[string]json.RawMessage) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}
