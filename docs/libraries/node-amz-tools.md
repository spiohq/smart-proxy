# Node.js  -  amz-tools/amazon-sp-api

Integration guide for using Smart Proxy with the [amz-tools/amazon-sp-api](https://github.com/amz-tools/amazon-sp-api) Node.js library.

## Overview

The `amazon-sp-api` library uses Node.js native `https` module for HTTP requests. The main `SellingPartner` class accepts a config object at construction time and exposes a `callAPI()` method for individual requests.

Custom headers are **natively supported** via the `headers` parameter in `callAPI()`. There is no built-in option to override the base URL, but this can be worked around.

## Sending the `X-SP-Proxy-Merchant-Id` Header

### Option A: Per-Call Headers (Built-in)

The `callAPI()` method accepts a `headers` object that is merged into the request:

```javascript
const SellingPartner = require("amazon-sp-api");

const spClient = new SellingPartner({
  region: "eu",
  refresh_token: "Atzr|xxx",
  credentials: {
    SELLING_PARTNER_APP_CLIENT_ID: "amzn1.application-oa2-client.xxx",
    SELLING_PARTNER_APP_CLIENT_SECRET: "your-secret",
  },
});

const orders = await spClient.callAPI({
  operation: "getOrders",
  endpoint: "orders",
  query: {
    MarketplaceIds: ["A1PA6795UKMFR9"],
    CreatedAfter: "2024-01-01T00:00:00Z",
  },
  headers: {
    "X-SP-Proxy-Merchant-Id": "YOUR_SELLER_ID",
  },
});
```

### Option B: Wrapper Function (Recommended)

To avoid repeating the header on every call, create a thin wrapper:

```javascript
const MERCHANT_ID = "YOUR_SELLER_ID";

function callAPI(params) {
  return spClient.callAPI({
    ...params,
    headers: {
      "X-SP-Proxy-Merchant-Id": MERCHANT_ID,
      ...(params.headers || {}),
    },
  });
}

// Usage
const orders = await callAPI({
  operation: "getOrders",
  endpoint: "orders",
  query: {
    MarketplaceIds: ["A1PA6795UKMFR9"],
    CreatedAfter: "2024-01-01T00:00:00Z",
  },
});
```

### Option C: Subclass SellingPartner

For a more structured approach, extend the class:

```javascript
const SellingPartner = require("amazon-sp-api");

class ProxiedSellingPartner extends SellingPartner {
  constructor(config, merchantId) {
    super(config);
    this._merchantId = merchantId;
  }

  async callAPI(params) {
    return super.callAPI({
      ...params,
      headers: {
        "X-SP-Proxy-Merchant-Id": this._merchantId,
        ...(params.headers || {}),
      },
    });
  }
}

const spClient = new ProxiedSellingPartner(
  {
    region: "eu",
    refresh_token: "Atzr|xxx",
    credentials: {
      SELLING_PARTNER_APP_CLIENT_ID: "amzn1.application-oa2-client.xxx",
      SELLING_PARTNER_APP_CLIENT_SECRET: "your-secret",
    },
  },
  "YOUR_SELLER_ID"
);
```

## Pointing the Library at Smart Proxy

The base URL is hardcoded in the `Request` class as `sellingpartnerapi-{region}.amazon.com` and cannot be overridden via configuration. Use the built-in `https_proxy_agent` option or patch the endpoint.

### Option A: HTTPS Proxy Agent

The library supports a custom `https_proxy_agent`, which can route traffic through a proxy. However, Smart Proxy is a **reverse proxy** (not a forward proxy), so this approach requires an additional forward proxy layer and is **not recommended**.

### Option B: Patch the Internal Endpoint (Recommended)

After instantiation, override the internal `_api_endpoint` on the request object:

```javascript
const SellingPartner = require("amazon-sp-api");

const PROXY_PORTS = {
  eu: 8080,
  na: 8081,
  fe: 8082,
};

class ProxiedSellingPartner extends SellingPartner {
  constructor(config, merchantId, proxyHost = "localhost") {
    super(config);
    this._merchantId = merchantId;
    this._proxyHost = proxyHost;
    this._proxyPort = PROXY_PORTS[config.region] || 8080;

    // Override the internal endpoint used for requests
    if (this._request) {
      this._request._api_endpoint = `${proxyHost}:${this._proxyPort}`;
    }
  }

  async callAPI(params) {
    // Ensure endpoint stays overridden (tokens refresh may recreate it)
    if (this._request) {
      this._request._api_endpoint = `${this._proxyHost}:${this._proxyPort}`;
    }
    return super.callAPI({
      ...params,
      headers: {
        "X-SP-Proxy-Merchant-Id": this._merchantId,
        ...(params.headers || {}),
      },
    });
  }
}
```

> **Note:** Since the library uses `https.request()` internally, pointing it to an HTTP endpoint (Smart Proxy) requires either running Smart Proxy with TLS or using a small fork to switch from `https` to `http`. See the full example below.

### Option C: Fork and Patch `Request.js`

For full control, modify `lib/Request.js`:

```javascript
// In _constructRequestOptions(), change:
this._api_endpoint = `${sandbox_prefix}sellingpartnerapi-${this._region}.amazon.com`;

// To:
this._api_endpoint = this._options.custom_endpoint
  || `${sandbox_prefix}sellingpartnerapi-${this._region}.amazon.com`;
```

Then pass it via options:

```javascript
const spClient = new SellingPartner({
  region: "eu",
  refresh_token: "Atzr|xxx",
  options: {
    custom_endpoint: "localhost:8080",
  },
});
```

### Region-to-Port Mapping

| Region | Smart Proxy Port |
|--------|------------------|
| `eu`   | `8080`           |
| `na`   | `8081`           |
| `fe`   | `8082`           |

## Full Example

```javascript
const SellingPartner = require("amazon-sp-api");

const PROXY_PORTS = { eu: 8080, na: 8081, fe: 8082 };

class ProxiedSellingPartner extends SellingPartner {
  constructor(config, merchantId, proxyHost = "localhost") {
    super(config);
    this._merchantId = merchantId;
    this._proxyHost = proxyHost;
    this._proxyPort = PROXY_PORTS[config.region] || 8080;
  }

  async callAPI(params) {
    return super.callAPI({
      ...params,
      headers: {
        "X-SP-Proxy-Merchant-Id": this._merchantId,
        ...(params.headers || {}),
      },
    });
  }
}

// Usage
const spClient = new ProxiedSellingPartner(
  {
    region: "eu",
    refresh_token: "Atzr|xxx",
    credentials: {
      SELLING_PARTNER_APP_CLIENT_ID: "amzn1.application-oa2-client.xxx",
      SELLING_PARTNER_APP_CLIENT_SECRET: "your-secret",
    },
  },
  "YOUR_SELLER_ID"
);

const orders = await spClient.callAPI({
  operation: "getOrders",
  endpoint: "orders",
  query: {
    MarketplaceIds: ["A1PA6795UKMFR9"],
    CreatedAfter: "2024-01-01T00:00:00Z",
  },
});

console.log(orders);
```

## Optional Proxy Headers

Pass additional Smart Proxy headers the same way:

```javascript
const response = await spClient.callAPI({
  operation: "getOrders",
  endpoint: "orders",
  query: { /* ... */ },
  headers: {
    "X-SP-Proxy-Merchant-Id": "YOUR_SELLER_ID",
    // "X-SP-Proxy-No-Cache": "true",       // bypass cache
    // "X-SP-Proxy-Cache-TTL": "10m",        // custom TTL
    // "X-SP-Proxy-Priority": "high",        // queue priority
  },
});
```

See the main [README](../../README.md#request-headers) for all available headers.
