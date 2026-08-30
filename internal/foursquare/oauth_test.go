package foursquare_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tk0miya/travelmap/internal/foursquare"
)

// TestAuthenticateURL pins the query the browser is sent to Foursquare with.
func TestAuthenticateURL(t *testing.T) {
	t.Parallel()

	got := foursquare.AuthenticateURL("client-id", "https://travelmap.example/foursquare/oauth/callback", "the-state")

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("AuthenticateURL returned an unparseable URL: %v", err)
	}

	want := url.Values{
		"client_id":     {"client-id"},
		"response_type": {"code"},
		"redirect_uri":  {"https://travelmap.example/foursquare/oauth/callback"},
		"state":         {"the-state"},
	}

	if got := u.Query(); got.Encode() != want.Encode() {
		t.Errorf("query = %q, want %q", got.Encode(), want.Encode())
	}

	if got := u.Scheme + "://" + u.Host + u.Path; got != "https://foursquare.com/oauth2/authenticate" {
		t.Errorf("base URL = %q, want the authenticate endpoint unchanged", got)
	}
}

// TestExchangeCode covers a successful exchange, and the two ways a fake
// server can fail one.
func TestExchangeCode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body       string
		status     int
		wantErr    bool
		wantToken  string
		wantFields []string
	}{
		"a bare access_token": {
			body:       `{"access_token": "the-token"}`,
			status:     http.StatusOK,
			wantToken:  "the-token",
			wantFields: []string{"access_token"},
		},
		"extra fields alongside it": {
			body:       `{"access_token": "the-token", "token_type": "bearer"}`,
			status:     http.StatusOK,
			wantToken:  "the-token",
			wantFields: []string{"access_token", "token_type"},
		},
		"no access_token at all": {
			body:    `{}`,
			status:  http.StatusOK,
			wantErr: true,
		},
		"a non-200 status": {
			body:    `{"error": "invalid_grant"}`,
			status:  http.StatusBadRequest,
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var gotQuery url.Values

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query()
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)

			token, err := foursquare.ExchangeCode(t.Context(), srv.Client(), srv.URL,
				"client-id", "client-secret", "https://travelmap.example/callback", "the-code")

			if tt.wantErr {
				if err == nil {
					t.Fatal("ExchangeCode returned nil error, want one")
				}

				return
			}

			if err != nil {
				t.Fatalf("ExchangeCode returned %v", err)
			}

			if token.AccessToken != tt.wantToken {
				t.Errorf("AccessToken = %q, want %q", token.AccessToken, tt.wantToken)
			}

			if strings.Join(token.Fields, ",") != strings.Join(tt.wantFields, ",") {
				t.Errorf("Fields = %v, want %v", token.Fields, tt.wantFields)
			}

			for _, want := range []string{"client_id", "client_secret", "grant_type", "redirect_uri", "code"} {
				if gotQuery.Get(want) == "" {
					t.Errorf("the request carried no %s parameter", want)
				}
			}

			if got := gotQuery.Get("grant_type"); got != "authorization_code" {
				t.Errorf("grant_type = %q, want authorization_code", got)
			}
		})
	}
}

// TestGetSelfUserID covers a successful call, and its own two ways to fail.
func TestGetSelfUserID(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body    string
		status  int
		wantErr bool
		want    string
	}{
		"an ordinary response": {
			body:   `{"meta": {"code": 200}, "response": {"user": {"id": "1709193"}}}`,
			status: http.StatusOK,
			want:   "1709193",
		},
		"no user id at all": {
			body:    `{"meta": {"code": 200}, "response": {}}`,
			status:  http.StatusOK,
			wantErr: true,
		},
		"a non-200 status": {
			body:    `{"meta": {"code": 401, "errorType": "not_authorized"}}`,
			status:  http.StatusUnauthorized,
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var gotAuth, gotVersion string

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				gotVersion = r.URL.Query().Get("v")

				if r.URL.Path != "/v2/users/self" {
					t.Errorf("path = %q, want /v2/users/self", r.URL.Path)
				}

				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)

			got, err := foursquare.GetSelfUserID(t.Context(), srv.Client(), srv.URL, "the-token")

			if tt.wantErr {
				if err == nil {
					t.Fatal("GetSelfUserID returned nil error, want one")
				}

				return
			}

			if err != nil {
				t.Fatalf("GetSelfUserID returned %v", err)
			}

			if got != tt.want {
				t.Errorf("GetSelfUserID = %q, want %q", got, tt.want)
			}

			if gotAuth != "Bearer the-token" {
				t.Errorf("Authorization = %q, want a Bearer header carrying the token", gotAuth)
			}

			if gotVersion == "" {
				t.Error("the request carried no v parameter")
			}
		})
	}
}
