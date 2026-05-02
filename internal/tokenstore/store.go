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
type Store struct {
	mu      sync.RWMutex
	entries map[string]entry
	ttl     time.Duration
}

// New creates a Store with the default TTL. 55 minutes matches the upstream
// access-token lifetime with a small safety margin.
func New() *Store {
	return NewWithTTL(defaultTTL)
}

func NewWithTTL(ttl time.Duration) *Store {
	return &Store{
		entries: make(map[string]entry),
		ttl:     ttl,
	}
}

func (s *Store) Set(merchantKey, token string) {
	if merchantKey == "" || token == "" {
		return
	}
	s.mu.Lock()
	s.entries[merchantKey] = entry{token: token, seenAt: time.Now()}
	s.mu.Unlock()
}

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

// UnavailabilityReason returns a human-readable explanation of why no valid
// token is available. Returns "" if a live token exists (callers should call
// Get first).
func (s *Store) UnavailabilityReason(merchantKey string) string {
	s.mu.RLock()
	e, ok := s.entries[merchantKey]
	s.mu.RUnlock()
	if !ok {
		return "No valid token available for this merchant. Send a new request through your client to activate replay."
	}
	if time.Since(e.seenAt) < s.ttl {
		return ""
	}
	age := time.Since(e.seenAt)
	return fmt.Sprintf("Token expired (last seen %.0f minutes ago, limit is %.0f). Send a new request through your client to refresh it.", age.Minutes(), s.ttl.Minutes())
}
