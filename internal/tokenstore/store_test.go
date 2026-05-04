package tokenstore_test

import (
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/tokenstore"
)

func TestStore_SetAndGet_ReturnsToken(t *testing.T) {
	s := tokenstore.New()
	s.Set("merchant-a", "tok-123")
	tok, ok := s.Get("merchant-a")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tok != "tok-123" {
		t.Fatalf("got %q, want %q", tok, "tok-123")
	}
}

func TestStore_Get_MissingKey_ReturnsFalse(t *testing.T) {
	s := tokenstore.New()
	_, ok := s.Get("no-such-merchant")
	if ok {
		t.Fatal("expected ok=false for missing key")
	}
}

func TestStore_Get_ExpiredEntry_ReturnsFalse(t *testing.T) {
	s := tokenstore.NewWithTTL(10 * time.Millisecond)
	s.Set("merchant-b", "tok-old")
	time.Sleep(20 * time.Millisecond)
	_, ok := s.Get("merchant-b")
	if ok {
		t.Fatal("expected ok=false for expired entry")
	}
}

func TestStore_Set_OverwritesExisting(t *testing.T) {
	s := tokenstore.New()
	s.Set("merchant-c", "tok-first")
	s.Set("merchant-c", "tok-second")
	tok, ok := s.Get("merchant-c")
	if !ok || tok != "tok-second" {
		t.Fatalf("got %q, want %q", tok, "tok-second")
	}
}

func TestStore_UnavailabilityReason_NeverSeen(t *testing.T) {
	s := tokenstore.New()
	reason := s.UnavailabilityReason("merchant-x")
	if reason == "" {
		t.Fatal("expected non-empty reason for unseen merchant")
	}
}

func TestStore_UnavailabilityReason_Expired(t *testing.T) {
	s := tokenstore.NewWithTTL(10 * time.Millisecond)
	s.Set("merchant-y", "tok-exp")
	time.Sleep(20 * time.Millisecond)
	reason := s.UnavailabilityReason("merchant-y")
	if reason == "" {
		t.Fatal("expected non-empty reason for expired merchant")
	}
}
