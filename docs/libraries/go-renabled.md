# Go  -  renabled/amzn-sp-api-go

Integration guide for using Smart Proxy with the [renabled/amzn-sp-api-go](https://github.com/renabled/amzn-sp-api-go) Go library.

## Overview

The `amzn-sp-api-go` library is a **go-swagger** generated SDK. It consists of two layers:

1. **`selling_partner` package**  -  handles authentication (OAuth2, AWS SigV4 signing) and provides a `ClientTransport()` method that returns a `go-openapi/runtime/client.Runtime`
2. **Generated API clients** (e.g. `orders_v0_client`)  -  accept a `runtime.ClientTransport` and make the actual API calls

HTTP requests flow through an `http.RoundTripper` chain: your code → OAuth2 transport → custom `transport` (SigV4 signing) → `http.DefaultTransport`. Custom headers are injected by wrapping this chain with your own `RoundTripper`.

## Sending the `X-SP-Proxy-Merchant-Id` Header

### `http.RoundTripper` Wrapper (Recommended)

Create a `RoundTripper` that injects the header into every request before passing it down the chain:

```go
package main

import "net/http"

// headerInjector wraps an http.RoundTripper and adds custom headers.
type headerInjector struct {
	next    http.RoundTripper
	headers map[string]string
}

func (h *headerInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	return h.next.RoundTrip(req)
}
```

### Integrating with the SDK

The SDK's `ClientTransport()` method (in `selling_partner/runtime.go`) builds the full transport chain internally and returns a `*client.Runtime`. Since the internal `transport` struct and its fields are unexported, the cleanest approach is to wrap the **outermost** `http.Client` used by the `client.Runtime`.

There are two ways to achieve this:

#### Option A: Fork `runtime.go` (Recommended)

Add a small hook to `ClientTransport()` that accepts a custom `http.RoundTripper`:

```go
// In selling_partner/runtime.go  -  add a new option:

func WithRoundTripper(rt http.RoundTripper) Opt {
	return func(o *option) {
		o.customTransport = rt
	}
}

// In the option struct, add:
type option struct {
	debug           bool
	isGrantless     bool
	customTransport http.RoundTripper  // <-- new
}

// In ClientTransport(), wrap the transport chain:
func (sp *Client) ClientTransport(ctx context.Context, opts ...Opt) *client.Runtime {
	// ... existing code ...

	base := &transport{
		aws4Signer: aws4Signer,
		region:     sp.config.region,
	}

	// If a custom transport is provided, chain it
	var roundTripper http.RoundTripper = &oauth2.Transport{
		Source: src,
		Base:   base,
	}
	if opt.customTransport != nil {
		base.originalTransport = opt.customTransport
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient,
		&http.Client{Transport: roundTripper},
	)

	cli := client.NewWithClient(sp.config.endpoint, "/", []string{"https"}, oauth2.NewClient(ctx, src))
	// ...
}
```

#### Option B: Wrap the Generated Client's Transport

Use the generated client directly with a custom `*client.Runtime` that has your headers injected:

```go
package main

import (
	"context"
	"fmt"
	"net/http"

	openapiclient "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"

	selling_partner "github.com/renabled/amzn-sp-api-go/selling_partner"
	orders "github.com/renabled/amzn-sp-api-go/api/ordersV0/orders_v0_client"
)

func main() {
	cfg := selling_partner.NewConfig(
		"AKIAIOSFODNN7EXAMPLE",
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"arn:aws:iam::123456789012:role/MyRole",
		"Atzr|xxx",
		"amzn1.application-oa2-client.xxx",
		"your-secret",
		"eu",
	)

	sp := selling_partner.New(cfg)
	ctx := context.Background()

	// Get the SDK's transport (includes OAuth2 + SigV4)
	transport := sp.ClientTransport(ctx)

	// The transport's underlying http.Client can be wrapped
	// by creating a new Runtime with custom default headers
	transport.DefaultAuthentication = nil // already handled by OAuth2
	transport.Transport = &headerInjector{
		next: transport.Transport,
		headers: map[string]string{
			"X-SP-Proxy-Merchant-Id": "YOUR_SELLER_ID",
		},
	}

	// Create the API client with the modified transport
	ordersClient := orders.New(transport, strfmt.Default)

	// Use it
	params := &orders_v0.GetOrdersParams{
		MarketplaceIds: []string{"A1PA6795UKMFR9"},
		Context:        ctx,
	}
	resp, err := ordersClient.OrdersV0.GetOrders(params)
	if err != nil {
		panic(err)
	}
	fmt.Println(resp.Payload)
}
```

## Pointing the Library at Smart Proxy

### Override Via Generated Client's `TransportConfig`

Each generated client has a `TransportConfig` with `WithHost()` and `WithSchemes()`:

```go
import orders "github.com/renabled/amzn-sp-api-go/api/ordersV0/orders_v0_client"

cfg := orders.DefaultTransportConfig().
	WithHost("localhost:8080").
	WithSchemes([]string{"http"})

ordersClient := orders.NewHTTPClientWithConfig(strfmt.Default, cfg)
```

However, this creates a client **without** the SDK's OAuth2/SigV4 auth chain. You need to combine it with the `selling_partner` transport  -  see the full example below.

### Override Via `selling_partner` Config (Fork Required)

The `Config.endpoint` field is unexported. To override it, either:

1. Export it: change `endpoint` to `Endpoint` in `config.go`
2. Or add a setter:

```go
// In selling_partner/config.go
func (c *Config) SetEndpoint(endpoint string) {
	c.endpoint = endpoint
}
```

Then:

```go
cfg := selling_partner.NewConfig(/* ... */)
cfg.SetEndpoint("localhost:8080")
```

### Region-to-Port Mapping

| Region | SP-API Endpoint | Smart Proxy Port |
|---|---|---|
| `eu` | `sellingpartnerapi-eu.amazon.com` | `8080` |
| `na` | `sellingpartnerapi-na.amazon.com` | `8081` |
| `fe` | `sellingpartnerapi-fe.amazon.com` | `8082` |

## Full Example (with Fork)

After exporting `Config.Endpoint` and adding the `WithRoundTripper` option:

```go
package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-openapi/strfmt"

	selling_partner "github.com/renabled/amzn-sp-api-go/selling_partner"
	orders "github.com/renabled/amzn-sp-api-go/api/ordersV0/orders_v0_client"
	orders_v0 "github.com/renabled/amzn-sp-api-go/api/ordersV0/orders_v0_client/orders_v0"
)

var proxyPorts = map[string]int{
	"EU": 8080,
	"NA": 8081,
	"FE": 8082,
}

type headerInjector struct {
	next    http.RoundTripper
	headers map[string]string
}

func (h *headerInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	return h.next.RoundTrip(req)
}

func main() {
	region := "eu"

	cfg := selling_partner.NewConfig(
		"AKIAIOSFODNN7EXAMPLE",
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"arn:aws:iam::123456789012:role/MyRole",
		"Atzr|xxx",
		"amzn1.application-oa2-client.xxx",
		"your-secret",
		region,
	)

	// Override endpoint to Smart Proxy (requires exported field or setter)
	port := proxyPorts["EU"]
	cfg.SetEndpoint(fmt.Sprintf("localhost:%d", port))

	sp := selling_partner.New(cfg)
	ctx := context.Background()

	// Get SDK transport with custom RoundTripper for the merchant header
	transport := sp.ClientTransport(ctx,
		selling_partner.WithRoundTripper(&headerInjector{
			headers: map[string]string{
				"X-SP-Proxy-Merchant-Id": "YOUR_SELLER_ID",
			},
		}),
	)

	// Create the orders client using the SDK transport
	ordersClient := orders.New(transport, strfmt.Default)

	params := &orders_v0.GetOrdersParams{
		MarketplaceIds: []string{"A1PA6795UKMFR9"},
		Context:        ctx,
	}

	resp, err := ordersClient.OrdersV0.GetOrders(params)
	if err != nil {
		panic(err)
	}

	fmt.Println(resp.Payload)
}
```

## Optional Proxy Headers

Add more Smart Proxy headers in the `headerInjector`:

```go
&headerInjector{
	headers: map[string]string{
		"X-SP-Proxy-Merchant-Id": "YOUR_SELLER_ID",
		// "X-SP-Proxy-No-Cache":  "true",       // bypass cache
		// "X-SP-Proxy-Cache-TTL": "10m",         // custom TTL
		// "X-SP-Proxy-Priority":  "high",        // queue priority
	},
}
```

See the main [README](../../README.md#request-headers) for all available headers.

## Note on Approach

This library has all config fields unexported (lowercase) and no public extension points for headers or endpoint overrides. **A lightweight fork is required** with two small changes:

1. Export `Config.endpoint` (or add a `SetEndpoint()` method)
2. Add a `WithRoundTripper()` option to `ClientTransport()`

Both changes are minimal and easy to maintain across upstream updates. The `http.RoundTripper` pattern is idiomatic Go and integrates cleanly with the existing OAuth2/SigV4 transport chain.
