# PHP  -  jlevers/selling-partner-api

Integration guide for using Smart Proxy with the [jlevers/selling-partner-api](https://github.com/jlevers/selling-partner-api) PHP library.

## Overview

The `jlevers/selling-partner-api` library is built on [Saloon](https://docs.saloon.dev/), an HTTP client abstraction layer. It uses a `SellingPartnerApi` connector class that extends `Saloon\Http\Connector`. This means all of Saloon's customization patterns (middleware, hooks, headers) are available.

## Sending the `X-SP-Proxy-Merchant-Id` Header

To get the best caching and rate-limit behavior from Smart Proxy, you should send `X-SP-Proxy-Merchant-Id` with every request. There are three ways to achieve this.

### Option A: Middleware (Recommended)

Create a Saloon request middleware and register it on the connector via the `boot()` method. This is the cleanest approach because it doesn't require subclassing the connector.

```php
use Saloon\Http\PendingRequest;

$connector = SellingPartnerApi::seller(
    clientId: 'amzn1.application-oa2-client.xxx',
    clientSecret: 'your-secret',
    refreshToken: 'Atzr|xxx',
    endpoint: Endpoint::EU,
);

// Add the header via middleware
$connector->middleware()->onRequest(function (PendingRequest $pendingRequest): void {
    $pendingRequest->headers()->add('X-SP-Proxy-Merchant-Id', 'YOUR_SELLER_ID');
});

// Use the connector as usual
$api = $connector->ordersV0();
$response = $api->getOrders(
    marketplaceIds: ['A1PA6795UKMFR9'],
    createdAfter: '2024-01-01T00:00:00Z',
);
```

### Option B: Override `defaultHeaders()`

Extend the `SellerConnector` (or `VendorConnector`) and add the header in `defaultHeaders()`:

```php
use SellingPartnerApi\Seller\SellerConnector;
use SellingPartnerApi\Enums\Endpoint;

class MySellerConnector extends SellerConnector
{
    public function __construct(
        string $clientId,
        string $clientSecret,
        string $refreshToken,
        Endpoint $endpoint,
        private string $merchantId,
        array $dataElements = [],
        ?string $delegatee = null,
        ?\GuzzleHttp\Client $authenticationClient = null,
        ?\SellingPartnerApi\Contracts\TokenCache $cache = null,
    ) {
        parent::__construct(
            $clientId, $clientSecret, $refreshToken, $endpoint,
            $dataElements, $delegatee, $authenticationClient, $cache,
        );
    }

    protected function defaultHeaders(): array
    {
        return array_merge(parent::defaultHeaders(), [
            'X-SP-Proxy-Merchant-Id' => $this->merchantId,
        ]);
    }
}
```

Usage:

```php
$connector = new MySellerConnector(
    clientId: 'amzn1.application-oa2-client.xxx',
    clientSecret: 'your-secret',
    refreshToken: 'Atzr|xxx',
    endpoint: Endpoint::EU,
    merchantId: 'YOUR_SELLER_ID',
);
```

### Option C: Override `handlePsrRequest()`

If you need to modify the final PSR-7 request object directly:

```php
use Psr\Http\Message\RequestInterface;
use Saloon\Http\PendingRequest;
use SellingPartnerApi\Seller\SellerConnector;

class MySellerConnector extends SellerConnector
{
    // ... constructor with $merchantId as above ...

    public function handlePsrRequest(RequestInterface $request, PendingRequest $pendingRequest): RequestInterface
    {
        $request = parent::handlePsrRequest($request, $pendingRequest);
        return $request->withHeader('X-SP-Proxy-Merchant-Id', $this->merchantId);
    }
}
```

## Pointing the Library at Smart Proxy

The library resolves the SP-API host from the `Endpoint` enum. To route traffic through Smart Proxy, you need to override the base URL on the connector.

### Using `resolveBaseUrl()`

```php
use SellingPartnerApi\Seller\SellerConnector;

class MySellerConnector extends SellerConnector
{
    public function resolveBaseUrl(): string
    {
        // Point to your local Smart Proxy instance (EU on port 8080)
        return 'http://localhost:8080';
    }

    // ... defaultHeaders override from above ...
}
```

### Region-to-Port Mapping

| Endpoint       | Smart Proxy Port |
|----------------|------------------|
| `Endpoint::EU` | `8080`           |
| `Endpoint::NA` | `8081`           |
| `Endpoint::FE` | `8082`           |

## Full Example

```php
use SellingPartnerApi\Seller\SellerConnector;
use SellingPartnerApi\Enums\Endpoint;
use Psr\Http\Message\RequestInterface;
use Saloon\Http\PendingRequest;

class ProxiedSellerConnector extends SellerConnector
{
    private const PROXY_PORTS = [
        'EU' => 8080,
        'NA' => 8081,
        'FE' => 8082,
    ];

    public function __construct(
        string $clientId,
        string $clientSecret,
        string $refreshToken,
        Endpoint $endpoint,
        private string $merchantId,
        private string $proxyHost = 'http://localhost',
        array $dataElements = [],
        ?string $delegatee = null,
        ?\GuzzleHttp\Client $authenticationClient = null,
        ?\SellingPartnerApi\Contracts\TokenCache $cache = null,
    ) {
        parent::__construct(
            $clientId, $clientSecret, $refreshToken, $endpoint,
            $dataElements, $delegatee, $authenticationClient, $cache,
        );
    }

    public function resolveBaseUrl(): string
    {
        $region = $this->endpoint->region();
        $port = self::PROXY_PORTS[$region] ?? 8080;
        return "{$this->proxyHost}:{$port}";
    }

    protected function defaultHeaders(): array
    {
        return array_merge(parent::defaultHeaders(), [
            'X-SP-Proxy-Merchant-Id' => $this->merchantId,
        ]);
    }
}

// Usage
$connector = new ProxiedSellerConnector(
    clientId: 'amzn1.application-oa2-client.xxx',
    clientSecret: 'your-secret',
    refreshToken: 'Atzr|xxx',
    endpoint: Endpoint::EU,
    merchantId: 'YOUR_SELLER_ID',
);

$response = $connector->ordersV0()->getOrders(
    marketplaceIds: ['A1PA6795UKMFR9'],
    createdAfter: '2024-01-01T00:00:00Z',
);
```

## Optional Proxy Headers

You can send additional Smart Proxy headers the same way:

```php
protected function defaultHeaders(): array
{
    return array_merge(parent::defaultHeaders(), [
        'X-SP-Proxy-Merchant-Id' => $this->merchantId,
        // 'X-SP-Proxy-No-Cache'  => 'true',          // bypass cache
        // 'X-SP-Proxy-Cache-TTL' => '10m',            // custom TTL
        // 'X-SP-Proxy-Priority'  => 'high',           // queue priority
    ]);
}
```

See the main [README](../../README.md#request-headers) for all available headers.
