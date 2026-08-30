package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// oauthStateBytes is how much randomness a state token carries — the same
// size as an API key ([auth.NewAPIKey]), since guessing one would let an
// attacker complete another user's OAuth flow.
const oauthStateBytes = 32

// oauthStateLifetime is how long a minted state is good for. Short on
// purpose: it only has to survive the redirect to Foursquare and back, not a
// session.
const oauthStateLifetime = 5 * time.Minute

// oauthStateStore mints and consumes the `state` value the Foursquare OAuth
// flow round-trips through the browser, bound to the travelmap user that
// started it.
//
// Held in process rather than in the database: one process serves this, and
// a state that does not survive a restart only costs the user a retry.
type oauthStateStore struct {
	mu     sync.Mutex
	states map[string]oauthStateEntry
}

// oauthStateEntry is one minted state: who it was minted for, and when it
// stops being usable.
type oauthStateEntry struct {
	userID int64
	expiry time.Time
}

// newOAuthStateStore returns an empty store.
func newOAuthStateStore() *oauthStateStore {
	return &oauthStateStore{states: make(map[string]oauthStateEntry)}
}

// New mints a state bound to userID, valid for [oauthStateLifetime] and
// usable exactly once — see [oauthStateStore.Consume].
func (s *oauthStateStore) New(userID int64) (string, error) {
	buf := make([]byte, oauthStateBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("httpapi: generating an OAuth state: %w", err)
	}

	token := hex.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.prune()
	s.states[token] = oauthStateEntry{userID: userID, expiry: time.Now().Add(oauthStateLifetime)}

	return token, nil
}

// Consume reports the user id token was minted for, and removes it — so a
// second callback presenting the same state, replayed or otherwise, finds
// nothing. false covers an unknown, expired or already-consumed state alike;
// the callback has no reason to tell those apart.
func (s *oauthStateStore) Consume(token string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.states[token]
	delete(s.states, token)

	if !ok || time.Now().After(entry.expiry) {
		return 0, false
	}

	return entry.userID, true
}

// prune drops every expired entry. Called with the lock already held, so
// that a state nobody ever completes does not accumulate forever on a
// server nobody restarts, without a ticker of its own for something this
// small.
func (s *oauthStateStore) prune() {
	now := time.Now()

	for token, entry := range s.states {
		if now.After(entry.expiry) {
			delete(s.states, token)
		}
	}
}
