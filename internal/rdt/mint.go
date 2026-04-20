package rdt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const tokensPath = "/tokens/2021-03-01/restrictedDataToken"

// CreateRDTRequest is the request body for the Tokens API.
type CreateRDTRequest struct {
	RestrictedResources []RestrictedResource `json:"restrictedResources"`
}

// CreateRDTResponse is the response body from the Tokens API.
// Note: the field is camelCase "expiresIn", not snake_case "expires_in"
// (unlike the LWA OAuth endpoint which uses OAuth convention).
type CreateRDTResponse struct {
	RestrictedDataToken string `json:"restrictedDataToken"`
	ExpiresIn           int    `json:"expiresIn"`
}

// Minter makes upstream createRestrictedDataToken calls to the SP-API.
type Minter struct {
	baseURL string
	client  *http.Client
}

// NewMinter creates a Minter that calls the Tokens API at the given base URL.
// The client should be configured with appropriate timeouts.
func NewMinter(baseURL string, client *http.Client) *Minter {
	return &Minter{
		baseURL: baseURL,
		client:  client,
	}
}

// Mint calls createRestrictedDataToken with the given LWA access token and
// restricted resource. Returns a CacheEntry with the RDT and its expiry time.
// The caller's LWA token is used as-is for authorization (the Tokens API
// requires a seller-authorized access token, not a grantless one).
func (m *Minter) Mint(lwaToken string, resource RestrictedResource) (CacheEntry, error) {
	reqBody := CreateRDTRequest{
		RestrictedResources: []RestrictedResource{resource},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return CacheEntry{}, fmt.Errorf("rdt mint: marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", m.baseURL+tokensPath, bytes.NewReader(body))
	if err != nil {
		return CacheEntry{}, fmt.Errorf("rdt mint: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-amz-access-token", lwaToken)

	resp, err := m.client.Do(req)
	if err != nil {
		return CacheEntry{}, fmt.Errorf("rdt mint: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return CacheEntry{}, fmt.Errorf("rdt mint: upstream returned %d: %s", resp.StatusCode, string(respBody))
	}

	var rdtResp CreateRDTResponse
	if err := json.NewDecoder(resp.Body).Decode(&rdtResp); err != nil {
		return CacheEntry{}, fmt.Errorf("rdt mint: decode response: %w", err)
	}

	if rdtResp.RestrictedDataToken == "" {
		return CacheEntry{}, fmt.Errorf("rdt mint: empty token in response")
	}

	return CacheEntry{
		Token:     rdtResp.RestrictedDataToken,
		ExpiresAt: time.Now().Add(time.Duration(rdtResp.ExpiresIn) * time.Second),
	}, nil
}
