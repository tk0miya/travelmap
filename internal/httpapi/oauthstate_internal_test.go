package httpapi

import "testing"

// TestOAuthStateStoreConsumeIsSingleUse pins that a state names its user
// exactly once: a second Consume of the same token, however it was
// presented again, finds nothing.
func TestOAuthStateStoreConsumeIsSingleUse(t *testing.T) {
	t.Parallel()

	s := newOAuthStateStore()

	token, err := s.New(42)
	if err != nil {
		t.Fatalf("New returned %v", err)
	}

	userID, ok := s.Consume(token)
	if !ok || userID != 42 {
		t.Fatalf("first Consume = (%d, %v), want (42, true)", userID, ok)
	}

	if _, ok := s.Consume(token); ok {
		t.Error("second Consume of the same token succeeded, want it consumed already")
	}
}

// TestOAuthStateStoreConsumeUnknown pins that a state this store never
// minted, or one from a different store, is refused rather than panicking.
func TestOAuthStateStoreConsumeUnknown(t *testing.T) {
	t.Parallel()

	s := newOAuthStateStore()

	if _, ok := s.Consume("not-a-real-token"); ok {
		t.Error("Consume of an unknown token succeeded, want it refused")
	}
}

// TestOAuthStateStoreConsumeExpired pins that a state past its lifetime is
// refused even though it was never consumed.
func TestOAuthStateStoreConsumeExpired(t *testing.T) {
	t.Parallel()

	s := newOAuthStateStore()

	token, err := s.New(1)
	if err != nil {
		t.Fatalf("New returned %v", err)
	}

	s.mu.Lock()
	entry := s.states[token]
	entry.expiry = entry.expiry.Add(-2 * oauthStateLifetime)
	s.states[token] = entry
	s.mu.Unlock()

	if _, ok := s.Consume(token); ok {
		t.Error("Consume of an expired token succeeded, want it refused")
	}
}

// TestOAuthStateStoreDistinctTokens pins that two states minted in a row
// for the same user do not collide.
func TestOAuthStateStoreDistinctTokens(t *testing.T) {
	t.Parallel()

	s := newOAuthStateStore()

	first, err := s.New(1)
	if err != nil {
		t.Fatalf("New returned %v", err)
	}

	second, err := s.New(1)
	if err != nil {
		t.Fatalf("New returned %v", err)
	}

	if first == second {
		t.Fatal("two calls to New returned the same token")
	}

	if userID, ok := s.Consume(first); !ok || userID != 1 {
		t.Errorf("Consume(first) = (%d, %v), want (1, true)", userID, ok)
	}

	if userID, ok := s.Consume(second); !ok || userID != 1 {
		t.Errorf("Consume(second) = (%d, %v), want (1, true)", userID, ok)
	}
}
