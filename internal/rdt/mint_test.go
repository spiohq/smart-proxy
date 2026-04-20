package rdt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinter_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/tokens/2021-03-01/restrictedDataToken", r.URL.Path)
		assert.Equal(t, "Atza|client-lwa-token", r.Header.Get("x-amz-access-token"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body CreateRDTRequest
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Len(t, body.RestrictedResources, 1)
		assert.Equal(t, "GET", body.RestrictedResources[0].Method)
		assert.Equal(t, "/orders/v0/orders/{orderId}", body.RestrictedResources[0].Path)
		assert.Equal(t, []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"}, body.RestrictedResources[0].DataElements)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "Atz.sprdt|fresh-rdt-token",
			ExpiresIn:           3600,
		})
	}))
	defer srv.Close()

	m := NewMinter(srv.URL, srv.Client())

	resource := RestrictedResource{
		Method:       "GET",
		Path:         "/orders/v0/orders/{orderId}",
		DataElements: []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"},
	}

	entry, err := m.Mint("Atza|client-lwa-token", resource)
	require.NoError(t, err)
	assert.Equal(t, "Atz.sprdt|fresh-rdt-token", entry.Token)
	// ExpiresAt should be roughly now + 3600s
	assert.WithinDuration(t, time.Now().Add(3600*time.Second), entry.ExpiresAt, 5*time.Second)
}

func TestMinter_NoDataElements(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body CreateRDTRequest
		json.NewDecoder(r.Body).Decode(&body)

		// dataElements should be omitted for non-Orders endpoints
		assert.Nil(t, body.RestrictedResources[0].DataElements)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "Atz.sprdt|mfn-rdt",
			ExpiresIn:           3600,
		})
	}))
	defer srv.Close()

	m := NewMinter(srv.URL, srv.Client())

	resource := RestrictedResource{
		Method: "GET",
		Path:   "/mfn/v0/shipments/{shipmentId}",
	}

	entry, err := m.Mint("Atza|lwa-token", resource)
	require.NoError(t, err)
	assert.Equal(t, "Atz.sprdt|mfn-rdt", entry.Token)
}

func TestMinter_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":[{"code":"Unauthorized","message":"Access denied"}]}`))
	}))
	defer srv.Close()

	m := NewMinter(srv.URL, srv.Client())

	resource := RestrictedResource{
		Method:       "GET",
		Path:         "/orders/v0/orders/{orderId}",
		DataElements: []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"},
	}

	_, err := m.Mint("Atza|bad-token", resource)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestMinter_MalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{not valid json`))
	}))
	defer srv.Close()

	m := NewMinter(srv.URL, srv.Client())

	resource := RestrictedResource{
		Method: "GET",
		Path:   "/mfn/v0/shipments/{shipmentId}",
	}

	_, err := m.Mint("Atza|lwa-token", resource)
	require.Error(t, err)
}

func TestMinter_EmptyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "",
			ExpiresIn:           3600,
		})
	}))
	defer srv.Close()

	m := NewMinter(srv.URL, srv.Client())

	resource := RestrictedResource{
		Method: "GET",
		Path:   "/mfn/v0/shipments/{shipmentId}",
	}

	_, err := m.Mint("Atza|lwa-token", resource)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestMinter_NetworkError(t *testing.T) {
	// Point to a server that doesn't exist
	m := NewMinter("http://127.0.0.1:1", &http.Client{Timeout: 100 * time.Millisecond})

	resource := RestrictedResource{
		Method: "GET",
		Path:   "/mfn/v0/shipments/{shipmentId}",
	}

	_, err := m.Mint("Atza|lwa-token", resource)
	require.Error(t, err)
}
