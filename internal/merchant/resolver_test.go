package merchant

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolve_Priority1_ExplicitHeader(t *testing.T) {
	r := NewResolver(nil)
	req, _ := http.NewRequest("GET", "/orders", nil)
	req.Header.Set("X-SP-Proxy-Merchant-Id", "my-merchant-eu")
	req.Header.Set("X-Amz-Access-Token", "Atza|some-token")

	m := r.Resolve(req)

	assert.Equal(t, "my-merchant-eu", m.Key)
	assert.Equal(t, "header", m.Source)
}

func TestResolve_Priority2_ConfigMap(t *testing.T) {
	tokenMap := map[string]string{
		"Atza|known-token": "configured-merchant",
	}
	r := NewResolver(tokenMap)
	req, _ := http.NewRequest("GET", "/orders", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|known-token")

	m := r.Resolve(req)

	assert.Equal(t, "configured-merchant", m.Key)
	assert.Equal(t, "config", m.Source)
}

func TestResolve_Priority3_TokenHash(t *testing.T) {
	r := NewResolver(nil)
	req, _ := http.NewRequest("GET", "/orders", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|unknown-token")

	m := r.Resolve(req)

	hash := sha256.Sum256([]byte("Atza|unknown-token"))
	expected := "tokenhash:" + hex.EncodeToString(hash[:16])
	assert.Equal(t, expected, m.Key)
	assert.Equal(t, "token-hash", m.Source)
}

func TestResolve_Priority1_TakesPrecedenceOverPriority2(t *testing.T) {
	tokenMap := map[string]string{
		"Atza|known": "from-config",
	}
	r := NewResolver(tokenMap)
	req, _ := http.NewRequest("GET", "/orders", nil)
	req.Header.Set("X-SP-Proxy-Merchant-Id", "from-header")
	req.Header.Set("X-Amz-Access-Token", "Atza|known")

	m := r.Resolve(req)

	assert.Equal(t, "from-header", m.Key)
	assert.Equal(t, "header", m.Source)
}

func TestResolve_EmptyToken_StillProducesHash(t *testing.T) {
	r := NewResolver(nil)
	req, _ := http.NewRequest("GET", "/orders", nil)
	// No token header at all

	m := r.Resolve(req)

	hash := sha256.Sum256([]byte(""))
	expected := "tokenhash:" + hex.EncodeToString(hash[:16])
	assert.Equal(t, expected, m.Key)
	assert.Equal(t, "token-hash", m.Source)
}

func TestResolve_HashIsDeterministic(t *testing.T) {
	r := NewResolver(nil)
	req1, _ := http.NewRequest("GET", "/orders", nil)
	req1.Header.Set("X-Amz-Access-Token", "Atza|same-token")
	req2, _ := http.NewRequest("GET", "/catalog", nil)
	req2.Header.Set("X-Amz-Access-Token", "Atza|same-token")

	m1 := r.Resolve(req1)
	m2 := r.Resolve(req2)

	assert.Equal(t, m1.Key, m2.Key, "same token must produce same hash")
}

func TestContextRoundtrip(t *testing.T) {
	original := ResolvedMerchant{Key: "test-merchant", Source: "header"}
	ctx := ContextWithMerchant(nil, original)

	recovered := MerchantFromContext(ctx)

	assert.Equal(t, original, recovered)
}

func TestMerchantFromContext_Missing(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	m := MerchantFromContext(req.Context())

	assert.Empty(t, m.Key)
	assert.Empty(t, m.Source)
}

// ── Strict mode (F-04) ───────────────────────────────────────────────────
// Strict mode rejects requests that lack a resolvable identity instead of
// merging them into the shared "tokenhash:e3b0c4..." (sha256 of "") bucket.

func TestResolveStrict_RejectsEmpty(t *testing.T) {
	r := NewResolver(nil)
	r.SetStrict(true)

	req, _ := http.NewRequest("GET", "/", nil)
	resolved, ok := r.ResolveStrict(req)
	assert.False(t, ok, "empty headers + empty token must not resolve in strict mode")
	assert.Empty(t, resolved.Key)
}

func TestResolveStrict_AcceptsExplicitMerchant(t *testing.T) {
	r := NewResolver(nil)
	r.SetStrict(true)

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-SP-Proxy-Merchant-Id", "MERCHANT-A")
	resolved, ok := r.ResolveStrict(req)
	assert.True(t, ok)
	assert.Equal(t, "MERCHANT-A", resolved.Key)
	assert.Equal(t, "header", resolved.Source)
}

func TestResolveStrict_AcceptsToken(t *testing.T) {
	// A non-empty access token is a valid identity even without an explicit
	// merchant header -- the token-hash fallback applies.
	r := NewResolver(nil)
	r.SetStrict(true)

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|some-token")
	resolved, ok := r.ResolveStrict(req)
	assert.True(t, ok)
	assert.Equal(t, "token-hash", resolved.Source)
	assert.NotEmpty(t, resolved.Key)
	assert.NotEqual(t, "tokenhash:", resolved.Key)
}

func TestMiddlewareStrict_Returns400OnEmpty(t *testing.T) {
	r := NewResolver(nil)
	r.SetStrict(true)

	called := false
	h := r.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req, _ := http.NewRequest("GET", "/", nil)
	rec := newResponseRecorder()
	h.ServeHTTP(rec, req)

	assert.False(t, called, "downstream handler must not run when merchant is unresolvable")
	assert.Equal(t, http.StatusBadRequest, rec.code)
}

func TestMiddlewareStrict_PassesWithMerchantHeader(t *testing.T) {
	r := NewResolver(nil)
	r.SetStrict(true)

	called := false
	h := r.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		m := MerchantFromContext(req.Context())
		assert.Equal(t, "M1", m.Key)
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-SP-Proxy-Merchant-Id", "M1")
	rec := newResponseRecorder()
	h.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.code)
}

func TestMiddleware_NonStrict_DefaultBehaviorUnchanged(t *testing.T) {
	// Backward-compat: by default Middleware fabricates the empty-token-hash
	// fallback. This test pins that behavior so a future "always strict"
	// drift fires immediately.
	r := NewResolver(nil) // strict NOT set

	var resolved ResolvedMerchant
	h := r.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		resolved = MerchantFromContext(req.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req, _ := http.NewRequest("GET", "/", nil)
	rec := newResponseRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.code)
	assert.Equal(t, "token-hash", resolved.Source)
	// sha256("")[:16] hex = "e3b0c44298fc1c149afbf4c8996fb924"
	assert.Equal(t, "tokenhash:e3b0c44298fc1c149afbf4c8996fb924", resolved.Key)
}

// minimal in-package response recorder so we don't depend on httptest.
type responseRecorder struct {
	code int
}

func newResponseRecorder() *responseRecorder { return &responseRecorder{code: http.StatusOK} }

func (r *responseRecorder) Header() http.Header        { return http.Header{} }
func (r *responseRecorder) Write(b []byte) (int, error) { return len(b), nil }
func (r *responseRecorder) WriteHeader(statusCode int)  { r.code = statusCode }
