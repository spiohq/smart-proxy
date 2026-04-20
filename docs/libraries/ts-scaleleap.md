# TypeScript  -  ScaleLeap/selling-partner-api-sdk

Integration guide for using Smart Proxy with the [ScaleLeap/selling-partner-api-sdk](https://github.com/ScaleLeap/selling-partner-api-sdk) TypeScript library.

## Overview

The `@scaleleap/selling-partner-api-sdk` is an OpenAPI-generated SDK using **axios** as its HTTP client. Each API client class (e.g. `OrdersApiClient`, `SellersApiClient`) accepts an `APIConfigurationParameters` object that supports:

- `basePath`  -  override the base URL
- `baseOptions`  -  pass axios request config including custom headers
- `axios`  -  inject a fully custom axios instance

Both custom headers and base URL override are **natively supported** without forking or subclassing.

## Sending the `X-SP-Proxy-Merchant-Id` Header

### Option A: `baseOptions.headers` (Recommended)

Pass custom headers via `baseOptions` in the configuration:

```typescript
import { OrdersApiClient } from "@scaleleap/selling-partner-api-sdk";

const client = new OrdersApiClient({
  accessToken: "Atza|xxx",
  region: "eu-west-1",
  baseOptions: {
    headers: {
      "X-SP-Proxy-Merchant-Id": "YOUR_SELLER_ID",
    },
  },
});

const response = await client.getOrders({
  MarketplaceIds: ["A1PA6795UKMFR9"],
  CreatedAfter: "2024-01-01T00:00:00Z",
});
```

Headers in `baseOptions.headers` are merged into every request made by the client.

### Option B: Custom Axios Instance

For full control, inject a pre-configured axios instance:

```typescript
import axios from "axios";
import { OrdersApiClient } from "@scaleleap/selling-partner-api-sdk";

const axiosInstance = axios.create({
  headers: {
    "X-SP-Proxy-Merchant-Id": "YOUR_SELLER_ID",
  },
});

const client = new OrdersApiClient({
  accessToken: "Atza|xxx",
  region: "eu-west-1",
  axios: axiosInstance,
});
```

### Option C: Axios Request Interceptor

For dynamic or conditional headers:

```typescript
import axios from "axios";
import { OrdersApiClient } from "@scaleleap/selling-partner-api-sdk";

const axiosInstance = axios.create();

axiosInstance.interceptors.request.use((config) => {
  config.headers["X-SP-Proxy-Merchant-Id"] = "YOUR_SELLER_ID";
  return config;
});

const client = new OrdersApiClient({
  accessToken: "Atza|xxx",
  region: "eu-west-1",
  axios: axiosInstance,
});
```

## Pointing the Library at Smart Proxy

### Using `basePath`

Override the endpoint directly in the configuration:

```typescript
const client = new OrdersApiClient({
  accessToken: "Atza|xxx",
  basePath: "http://localhost:8080", // Smart Proxy EU
  baseOptions: {
    headers: {
      "X-SP-Proxy-Merchant-Id": "YOUR_SELLER_ID",
    },
  },
});
```

### Region-to-Port Mapping

| Region       | SP-API Endpoint                        | Smart Proxy Port |
|--------------|----------------------------------------|------------------|
| `eu-west-1`  | `sellingpartnerapi-eu.amazon.com`      | `8080`           |
| `us-east-1`  | `sellingpartnerapi-na.amazon.com`      | `8081`           |
| `us-west-2`  | `sellingpartnerapi-fe.amazon.com`      | `8082`           |

## Full Example

```typescript
import {
  OrdersApiClient,
  ReportsApiClient,
  SellersApiClient,
  APIConfigurationParameters,
} from "@scaleleap/selling-partner-api-sdk";

const PROXY_PORTS: Record<string, number> = {
  "eu-west-1": 8080,
  "us-east-1": 8081,
  "us-west-2": 8082,
};

function createConfig(
  region: string,
  accessToken: string,
  merchantId: string,
  proxyHost = "http://localhost"
): APIConfigurationParameters {
  const port = PROXY_PORTS[region] ?? 8080;

  return {
    accessToken,
    basePath: `${proxyHost}:${port}`,
    baseOptions: {
      headers: {
        "X-SP-Proxy-Merchant-Id": merchantId,
      },
    },
  };
}

// Usage  -  config is reusable across all API clients
const config = createConfig("eu-west-1", "Atza|xxx", "YOUR_SELLER_ID");

const orders = new OrdersApiClient(config);
const reports = new ReportsApiClient(config);
const sellers = new SellersApiClient(config);

const ordersResponse = await orders.getOrders({
  MarketplaceIds: ["A1PA6795UKMFR9"],
  CreatedAfter: "2024-01-01T00:00:00Z",
});

const participations = await sellers.getMarketplaceParticipations();

console.log(ordersResponse.data);
```

## Optional Proxy Headers

```typescript
const config: APIConfigurationParameters = {
  accessToken: "Atza|xxx",
  basePath: "http://localhost:8080",
  baseOptions: {
    headers: {
      "X-SP-Proxy-Merchant-Id": "YOUR_SELLER_ID",
      // "X-SP-Proxy-No-Cache": "true",       // bypass cache
      // "X-SP-Proxy-Cache-TTL": "10m",        // custom TTL
      // "X-SP-Proxy-Priority": "high",        // queue priority
    },
  },
};
```

See the main [README](../../README.md#request-headers) for all available headers.
