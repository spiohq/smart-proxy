package merchant

import "context"

type contextKey string

const merchantContextKey contextKey = "sp-proxy-merchant"

// ContextWithMerchant returns a new context carrying the resolved merchant.
func ContextWithMerchant(parent context.Context, m ResolvedMerchant) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithValue(parent, merchantContextKey, m)
}

// MerchantFromContext extracts the resolved merchant from the context.
// Returns a zero-value ResolvedMerchant if not present.
func MerchantFromContext(ctx context.Context) ResolvedMerchant {
	m, _ := ctx.Value(merchantContextKey).(ResolvedMerchant)
	return m
}
