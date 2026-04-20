# PHP  -  amazon-php/sp-api-sdk (+ plentymarkets Fork)

Integration guide for using Smart Proxy with the [amazon-php/sp-api-sdk](https://github.com/amazon-php/sp-api-sdk) and the [plentymarkets fork](https://github.com/plentymarkets/sp-api-sdk).

## Overview

The `amazon-php/sp-api-sdk` is an auto-generated, PSR-compliant SDK. It uses:

- **PSR-18** `ClientInterface` for HTTP requests (you inject Guzzle, cURL client, etc.)
- **PSR-17** factories for request/stream creation
- **PSR-7** immutable request/response objects

The SDK is instantiated via `SellingPartnerSDK::create()`, which accepts the HTTP client as its first argument. This makes it straightforward to wrap the client with custom behavior.

### plentymarkets Fork Differences

The [plentymarkets fork](https://github.com/plentymarkets/sp-api-sdk) uses a different namespace (`Plenty\AmazonPHP\SellingPartner`) and adds a `CredentialsHandler` for automatic IAM role credential refresh. The integration patterns below work identically for both  -  just adjust the namespace.

| | amazon-php/sp-api-sdk | plentymarkets/sp-api-sdk |
|---|---|---|
| Namespace | `AmazonPHP\SellingPartner` | `Plenty\AmazonPHP\SellingPartner` |
| Credentials | Direct strings in `Configuration` | `CredentialsHandler` with auto-refresh |
| Sandbox mode | Supported | Not supported |
| Extension system | Yes | Yes (identical) |

## Sending the `X-SP-Proxy-Merchant-Id` Header

### Why Not Extensions?

The SDK has an `Extension` interface with a `preRequest()` hook, but it **cannot modify requests**. The method signature is `void`, and since PSR-7 requests are immutable, any `$request->withHeader(...)` call inside `preRequest()` creates a new object that is discarded. The original request is sent unchanged. Use `preRequest()` only for logging or monitoring.

### PSR-18 Client Wrapper (Recommended)

Wrap the PSR-18 HTTP client to inject headers (and optionally rewrite the URL) before every request:

```php
<?php

use Psr\Http\Client\ClientInterface;
use Psr\Http\Message\RequestInterface;
use Psr\Http\Message\ResponseInterface;

class SmartProxyClient implements ClientInterface
{
    public function __construct(
        private readonly ClientInterface $inner,
        private readonly string $merchantId,
        private readonly ?string $proxyBaseUrl = null,
    ) {}

    public function sendRequest(RequestInterface $request): ResponseInterface
    {
        // Add the merchant ID header
        $request = $request->withHeader('X-SP-Proxy-Merchant-Id', $this->merchantId);

        // Optionally rewrite the URL to point at Smart Proxy
        if ($this->proxyBaseUrl !== null) {
            $originalUri = $request->getUri();
            $proxyUri = $originalUri
                ->withScheme(parse_url($this->proxyBaseUrl, PHP_URL_SCHEME) ?: 'http')
                ->withHost(parse_url($this->proxyBaseUrl, PHP_URL_HOST) ?: 'localhost')
                ->withPort(parse_url($this->proxyBaseUrl, PHP_URL_PORT));

            $request = $request->withUri($proxyUri);
        }

        return $this->inner->sendRequest($request);
    }
}
```

### Usage

```php
use AmazonPHP\SellingPartner\Configuration;
use AmazonPHP\SellingPartner\SellingPartnerSDK;
use Buzz\Client\Curl;
use Nyholm\Psr7\Factory\Psr17Factory;
use Monolog\Logger;

$factory = new Psr17Factory();
$httpClient = new Curl($factory);
$logger = new Logger('sp-api');

$configuration = Configuration::forIAMUser(
    clientId: 'amzn1.application-oa2-client.xxx',
    clientSecret: 'your-secret',
    accessKey: 'AKIAIOSFODNN7EXAMPLE',
    secretKey: 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY',
);

// Wrap the HTTP client with Smart Proxy support
$proxyClient = new SmartProxyClient(
    inner: $httpClient,
    merchantId: 'YOUR_SELLER_ID',
    proxyBaseUrl: 'http://localhost:8080',  // EU region
);

$sdk = SellingPartnerSDK::create(
    $proxyClient,   // <-- wrapped client
    $factory,
    $factory,
    $configuration,
    $logger,
);

// Use the SDK as usual  -  all requests go through Smart Proxy
$orders = $sdk->orders()->getOrders(
    $accessToken,
    'eu-west-1',
    ['A1PA6795UKMFR9'],
    createdAfter: '2024-01-01T00:00:00Z',
);
```

### plentymarkets Fork Usage

The only difference is the namespace and credential setup:

```php
use Plenty\AmazonPHP\SellingPartner\Configuration;
use Plenty\AmazonPHP\SellingPartner\SellingPartnerSDK;

$configuration = Configuration::forIAMRole(
    clientId: 'amzn1.application-oa2-client.xxx',
    clientSecret: 'your-secret',
    roleArn: 'arn:aws:iam::123456789012:role/MyRole',
);

$proxyClient = new SmartProxyClient(
    inner: $httpClient,
    merchantId: 'YOUR_SELLER_ID',
    proxyBaseUrl: 'http://localhost:8080',
);

$sdk = SellingPartnerSDK::create($proxyClient, $factory, $factory, $configuration, $logger);
```

## Region-Aware Proxy Client

If you work with multiple regions, create a region-aware wrapper:

```php
class SmartProxyClient implements ClientInterface
{
    private const REGION_PORTS = [
        'sellingpartnerapi-eu.amazon.com'  => 8080,
        'sellingpartnerapi-na.amazon.com'  => 8081,
        'sellingpartnerapi.amazon.com'     => 8081,  // NA alias
        'sellingpartnerapi-fe.amazon.com'  => 8082,
    ];

    public function __construct(
        private readonly ClientInterface $inner,
        private readonly string $merchantId,
        private readonly string $proxyHost = 'localhost',
    ) {}

    public function sendRequest(RequestInterface $request): ResponseInterface
    {
        $request = $request->withHeader('X-SP-Proxy-Merchant-Id', $this->merchantId);

        // Resolve the correct proxy port from the original host
        $originalHost = $request->getUri()->getHost();
        $port = self::REGION_PORTS[$originalHost] ?? 8080;

        $proxyUri = $request->getUri()
            ->withScheme('http')
            ->withHost($this->proxyHost)
            ->withPort($port);

        $request = $request->withUri($proxyUri);

        return $this->inner->sendRequest($request);
    }
}
```

Usage:

```php
$proxyClient = new SmartProxyClient(
    inner: $httpClient,
    merchantId: 'YOUR_SELLER_ID',
    proxyHost: 'localhost',
);

// The correct port is resolved automatically from the region
$sdk = SellingPartnerSDK::create($proxyClient, $factory, $factory, $configuration, $logger);

$sdk->orders()->getOrders($token, 'eu-west-1', ...);  // → localhost:8080
$sdk->orders()->getOrders($token, 'us-east-1', ...);  // → localhost:8081
```

### Region-to-Port Mapping

| SP-API Host | Region | Smart Proxy Port |
|---|---|---|
| `sellingpartnerapi-eu.amazon.com` | EU | `8080` |
| `sellingpartnerapi-na.amazon.com` / `sellingpartnerapi.amazon.com` | NA | `8081` |
| `sellingpartnerapi-fe.amazon.com` | FE | `8082` |

## Optional Proxy Headers

Add more Smart Proxy headers in the `sendRequest()` method:

```php
public function sendRequest(RequestInterface $request): ResponseInterface
{
    $request = $request->withHeader('X-SP-Proxy-Merchant-Id', $this->merchantId);
    // $request = $request->withHeader('X-SP-Proxy-No-Cache', 'true');       // bypass cache
    // $request = $request->withHeader('X-SP-Proxy-Cache-TTL', '10m');       // custom TTL
    // $request = $request->withHeader('X-SP-Proxy-Priority', 'high');       // queue priority

    // ... rewrite URL ...

    return $this->inner->sendRequest($request);
}
```

See the main [README](../../README.md#request-headers) for all available headers.
