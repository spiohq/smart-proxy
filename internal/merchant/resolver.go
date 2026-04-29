package merchant

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
)

// ResolvedMerchant holds the resolved merchant identity for a request.
type ResolvedMerchant struct {
	Key    string // Stable merchant identifier
	Source string // "header", "config", or "token-hash"
}

// Resolver resolves merchant identity from incoming requests using a 3-tier
// priority system. In strict mode (SetStrict(true)) the empty-token-hash
// fallback is suppressed: callers without an X-SP-Proxy-Merchant-Id header
// AND without an X-Amz-Access-Token are rejected with 400 instead of being
// merged into the shared "tokenhash:e3b0c4..." (sha256 of "") bucket.
//
// Pentest finding F-04.
type Resolver struct {
	tokenMap map[string]string // access_token → merchant_key
	strict   bool
}

// NewResolver creates a new merchant resolver. tokenMap may be nil.
func NewResolver(tokenMap map[string]string) *Resolver {
	return &Resolver{tokenMap: tokenMap}
}

// SetStrict enables strict mode: ResolveStrict signals "no identity" for
// header-and-token-less requests, and Middleware short-circuits with 400
// instead of fabricating a tokenhash:e3b0c4... key shared by all such
// callers.
func (r *Resolver) SetStrict(v bool) { r.strict = v }

// Strict reports whether strict mode is enabled.
func (r *Resolver) Strict() bool { return r.strict }

// Resolve identifies the merchant for a request.
//
// Priority:
//  1. X-SP-Proxy-Merchant-Id header (explicit)
//  2. Config-based token mapping (if tokenMap is set)
//  3. SHA-256 hash of the access token (fallback, always works)
func (r *Resolver) Resolve(req *http.Request) ResolvedMerchant {
	// Priority 1: Explicit header
	if id := req.Header.Get("X-SP-Proxy-Merchant-Id"); id != "" {
		return ResolvedMerchant{Key: id, Source: "header"}
	}

	// Priority 2: Config-based token mapping
	token := req.Header.Get("X-Amz-Access-Token")
	if r.tokenMap != nil {
		if mapped, ok := r.tokenMap[token]; ok {
			return ResolvedMerchant{Key: mapped, Source: "config"}
		}
	}

	// Priority 3: Token hash fallback
	hash := sha256.Sum256([]byte(token))
	return ResolvedMerchant{
		Key:    "tokenhash:" + hex.EncodeToString(hash[:16]),
		Source: "token-hash",
	}
}

// ResolveStrict is a strict variant of Resolve. Returns (zero, false) when
// neither X-SP-Proxy-Merchant-Id nor X-Amz-Access-Token is present, instead
// of falling back to the shared empty-token-hash bucket.
func (r *Resolver) ResolveStrict(req *http.Request) (ResolvedMerchant, bool) {
	if id := req.Header.Get("X-SP-Proxy-Merchant-Id"); id != "" {
		return ResolvedMerchant{Key: id, Source: "header"}, true
	}
	token := req.Header.Get("X-Amz-Access-Token")
	if r.tokenMap != nil {
		if mapped, ok := r.tokenMap[token]; ok {
			return ResolvedMerchant{Key: mapped, Source: "config"}, true
		}
	}
	if token == "" {
		// No identity at all -- refuse rather than merge into the shared
		// empty-token-hash bucket.
		return ResolvedMerchant{}, false
	}
	hash := sha256.Sum256([]byte(token))
	return ResolvedMerchant{
		Key:    "tokenhash:" + hex.EncodeToString(hash[:16]),
		Source: "token-hash",
	}, true
}

// Middleware returns an HTTP middleware that resolves the merchant and stores
// it in the request context. In strict mode, requests that lack a resolvable
// merchant identity receive 400 Bad Request instead of being merged into the
// shared empty-token-hash bucket (F-04).
func (r *Resolver) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if r.strict {
				m, ok := r.ResolveStrict(req)
				if !ok {
					http.Error(w,
						"merchant identity required: set X-SP-Proxy-Merchant-Id or X-Amz-Access-Token",
						http.StatusBadRequest)
					return
				}
				ctx := ContextWithMerchant(req.Context(), m)
				next.ServeHTTP(w, req.WithContext(ctx))
				return
			}
			m := r.Resolve(req)
			ctx := ContextWithMerchant(req.Context(), m)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}
