package tokenstore

import (
	"fmt"
	"sync"
	"time"
)

const defaultTTL = 55 * time.Minute

type entry struct {
	token  string
	seenAt time.Time
}

// Store holds the most recently seen x-amz-access-token per merchant key.
// Tokens are kept in RAM only and expire after 55 minutes (same safety margin
// as the RDT cache). This follows the pattern established in internal/rdt/cache.go.
type Store struct {
	mu      sync.RWMutex
	entries map[string]entry
	ttl     time.Duration
}

// New creates a Store with the default 55-minute TTL.
func New() *Store {
	return NewWithTTL(defaultTTL)
}

// NewWithTTL creates a Store with a custom TTL. Used in tests.
func NewWithTTL(ttl time.Duration) *Store {
	return &Store{
		entries: make(map[string]entry),
		ttl:     ttl,
	}
}

// Set records the most recently seen token for merchantKey.
func (s *Store) Set(merchantKey, token string) {
	if merchantKey == "" || token == "" {
		return
	}
	s.mu.Lock()
	s.entries[merchantKey] = entry{token: token, seenAt: time.Now()}
	s.mu.Unlock()
}

// Get returns the token for merchantKey if one exists and has not expired.
func (s *Store) Get(merchantKey string) (string, bool) {
	s.mu.RLock()
	e, ok := s.entries[merchantKey]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Since(e.seenAt) >= s.ttl {
		s.mu.Lock()
		delete(s.entries, merchantKey)
		s.mu.Unlock()
		return "", false
	}
	return e.token, true
}

// UnavailabilityReason returns a human-readable English explanation of why
// no valid token is available for merchantKey. Returns "" if a token IS
// available (callers should call Get first).
func (s *Store) UnavailabilityReason(merchantKey string) string {
	s.mu.RLock()
	e, ok := s.entries[merchantKey]
	s.mu.RUnlock()
	if !ok {
		return "No valid token available for this merchant. Send a new request through your client to activate replay."
	}
	age := time.Since(e.seenAt)
	return fmt.Sprintf("Token expired (last seen %.0f minutes ago, limit is 55). Send a new request through your client to refresh it.", age.Minutes())
}
