package model_test

import (
	"testing"

	"github.com/tk0miya/travelmap/internal/model"
)

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		raw     string
		want    string
		wantErr bool
	}{
		"an address as typed":      {raw: "alice@example.com", want: "alice@example.com"},
		"an address with capitals": {raw: "Alice@Example.COM", want: "alice@example.com"},
		// A copy-and-paste from a terminal or a browser brings whitespace with
		// it, and an address that differs by a trailing space is an account
		// nobody can log in to.
		"surrounding whitespace": {raw: "  alice@example.com\n", want: "alice@example.com"},
		"a subaddress":           {raw: "alice+phone@example.com", want: "alice+phone@example.com"},
		"empty":                  {raw: "", wantErr: true},
		"whitespace only":        {raw: "   ", wantErr: true},
		"no at sign":             {raw: "alice", wantErr: true},
		// Accepted by net/mail as an address with a display name, but a login
		// identity is the address alone.
		"a display name": {raw: "Alice <alice@example.com>", wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := model.NormalizeEmail(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeEmail(%q) = %q, want an error", tt.raw, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("NormalizeEmail(%q) returned %v", tt.raw, err)
			}

			if got != tt.want {
				t.Errorf("NormalizeEmail(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
