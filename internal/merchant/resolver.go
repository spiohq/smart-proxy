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
// priority system.
type Resolver struct {
	tokenMap map[string]string // access_token → merchant_key
}

// NewResolver creates a new merchant resolver. tokenMap may be nil.
func NewResolver(tokenMap map[string]string) *Resolver {
	return &Resolver{tokenMap: tokenMap}
}

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

// Middleware returns an HTTP middleware that resolves the merchant and stores
// it in the request context.
func (r *Resolver) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			m := r.Resolve(req)
			ctx := ContextWithMerchant(req.Context(), m)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}
