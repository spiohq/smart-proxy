package rdt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCache_SetAndGet(t *testing.T) {
	c := NewCache(5 * time.Minute)

	entry := CacheEntry{
		Token:     "Atz.sprdt|test-token-123",
		ExpiresAt: time.Now().Add(55 * time.Minute),
	}
	key := CacheKey{
		MerchantID:   "merchant-1",
		GenericPath:  "/orders/v0/orders/{orderId}",
		DataElements: "buyerInfo,shippingAddress,buyerTaxInformation",
	}

	c.Set(key, entry)

	got, ok := c.Get(key)
	require.True(t, ok)
	assert.Equal(t, entry.Token, got.Token)
}

func TestCache_Miss(t *testing.T) {
	c := NewCache(5 * time.Minute)

	key := CacheKey{
		MerchantID:   "merchant-1",
		GenericPath:  "/orders/v0/orders/{orderId}",
		DataElements: "buyerInfo,shippingAddress,buyerTaxInformation",
	}

	_, ok := c.Get(key)
	assert.False(t, ok)
}

func TestCache_Expired(t *testing.T) {
	c := NewCache(5 * time.Minute)

	key := CacheKey{
		MerchantID:   "merchant-1",
		GenericPath:  "/orders/v0/orders/{orderId}",
		DataElements: "buyerInfo,shippingAddress,buyerTaxInformation",
	}
	entry := CacheEntry{
		Token:     "Atz.sprdt|expired-token",
		ExpiresAt: time.Now().Add(-1 * time.Minute), // already expired
	}

	c.Set(key, entry)

	_, ok := c.Get(key)
	assert.False(t, ok, "expired entries should not be returned")
}

func TestCache_SafetyMargin(t *testing.T) {
	margin := 5 * time.Minute
	c := NewCache(margin)

	key := CacheKey{
		MerchantID:   "merchant-1",
		GenericPath:  "/mfn/v0/shipments/{shipmentId}",
		DataElements: "",
	}

	// Token expires in 4 minutes, but safety margin is 5 minutes
	// -> should be treated as expired
	entry := CacheEntry{
		Token:     "Atz.sprdt|almost-expired",
		ExpiresAt: time.Now().Add(4 * time.Minute),
	}

	c.Set(key, entry)

	_, ok := c.Get(key)
	assert.False(t, ok, "entries within safety margin should not be returned")

	// Token expires in 6 minutes, safety margin is 5 minutes -> valid
	entry2 := CacheEntry{
		Token:     "Atz.sprdt|still-valid",
		ExpiresAt: time.Now().Add(6 * time.Minute),
	}

	c.Set(key, entry2)

	got, ok := c.Get(key)
	require.True(t, ok)
	assert.Equal(t, "Atz.sprdt|still-valid", got.Token)
}

func TestCache_Invalidate(t *testing.T) {
	c := NewCache(5 * time.Minute)

	key := CacheKey{
		MerchantID:   "merchant-1",
		GenericPath:  "/orders/v0/orders/{orderId}",
		DataElements: "buyerInfo,shippingAddress,buyerTaxInformation",
	}
	entry := CacheEntry{
		Token:     "Atz.sprdt|will-be-invalidated",
		ExpiresAt: time.Now().Add(55 * time.Minute),
	}

	c.Set(key, entry)

	// Verify it's there
	_, ok := c.Get(key)
	require.True(t, ok)

	// Invalidate
	c.Invalidate(key)

	// Should be gone
	_, ok = c.Get(key)
	assert.False(t, ok)
}

func TestCache_DifferentMerchantsSameOperation(t *testing.T) {
	c := NewCache(5 * time.Minute)

	key1 := CacheKey{
		MerchantID:   "merchant-1",
		GenericPath:  "/orders/v0/orders/{orderId}",
		DataElements: "buyerInfo,shippingAddress,buyerTaxInformation",
	}
	key2 := CacheKey{
		MerchantID:   "merchant-2",
		GenericPath:  "/orders/v0/orders/{orderId}",
		DataElements: "buyerInfo,shippingAddress,buyerTaxInformation",
	}

	c.Set(key1, CacheEntry{Token: "token-merchant-1", ExpiresAt: time.Now().Add(55 * time.Minute)})
	c.Set(key2, CacheEntry{Token: "token-merchant-2", ExpiresAt: time.Now().Add(55 * time.Minute)})

	got1, ok := c.Get(key1)
	require.True(t, ok)
	assert.Equal(t, "token-merchant-1", got1.Token)

	got2, ok := c.Get(key2)
	require.True(t, ok)
	assert.Equal(t, "token-merchant-2", got2.Token)
}

func TestCache_SameMerchantDifferentOperations(t *testing.T) {
	c := NewCache(5 * time.Minute)

	ordersKey := CacheKey{
		MerchantID:   "merchant-1",
		GenericPath:  "/orders/v0/orders/{orderId}",
		DataElements: "buyerInfo,shippingAddress,buyerTaxInformation",
	}
	mfnKey := CacheKey{
		MerchantID:   "merchant-1",
		GenericPath:  "/mfn/v0/shipments/{shipmentId}",
		DataElements: "",
	}

	c.Set(ordersKey, CacheEntry{Token: "orders-rdt", ExpiresAt: time.Now().Add(55 * time.Minute)})
	c.Set(mfnKey, CacheEntry{Token: "mfn-rdt", ExpiresAt: time.Now().Add(55 * time.Minute)})

	got1, _ := c.Get(ordersKey)
	assert.Equal(t, "orders-rdt", got1.Token)

	got2, _ := c.Get(mfnKey)
	assert.Equal(t, "mfn-rdt", got2.Token)
}

func TestCache_Size(t *testing.T) {
	c := NewCache(5 * time.Minute)
	assert.Equal(t, 0, c.Size())

	key := CacheKey{MerchantID: "m1", GenericPath: "/orders/v0/orders/{orderId}", DataElements: ""}
	c.Set(key, CacheEntry{Token: "tok", ExpiresAt: time.Now().Add(55 * time.Minute)})
	assert.Equal(t, 1, c.Size())

	c.Invalidate(key)
	assert.Equal(t, 0, c.Size())
}

func TestBuildCacheKey(t *testing.T) {
	// With dataElements
	k1 := BuildCacheKey("merchant-1", PIIOperation{
		GenericPath:  "/orders/v0/orders/{orderId}",
		DataElements: []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"},
	})
	assert.Equal(t, "merchant-1", k1.MerchantID)
	assert.Equal(t, "/orders/v0/orders/{orderId}", k1.GenericPath)
	assert.Equal(t, "buyerInfo,buyerTaxInformation,shippingAddress", k1.DataElements) // sorted

	// Without dataElements
	k2 := BuildCacheKey("merchant-1", PIIOperation{
		GenericPath:  "/mfn/v0/shipments/{shipmentId}",
		DataElements: nil,
	})
	assert.Equal(t, "", k2.DataElements)

	// Same operation, different merchant -> different keys
	k3 := BuildCacheKey("merchant-2", PIIOperation{
		GenericPath:  "/orders/v0/orders/{orderId}",
		DataElements: []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"},
	})
	assert.NotEqual(t, k1, k3)
}

func TestBuildCacheKey_SortsDataElements(t *testing.T) {
	// Order should not matter
	k1 := BuildCacheKey("m", PIIOperation{
		GenericPath:  "/orders/v0/orders/{orderId}",
		DataElements: []string{"shippingAddress", "buyerInfo", "buyerTaxInformation"},
	})
	k2 := BuildCacheKey("m", PIIOperation{
		GenericPath:  "/orders/v0/orders/{orderId}",
		DataElements: []string{"buyerTaxInformation", "buyerInfo", "shippingAddress"},
	})
	assert.Equal(t, k1, k2)
}
