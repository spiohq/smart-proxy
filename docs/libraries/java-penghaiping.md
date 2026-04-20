# Java  -  penghaiping/amazon-sp-api

Integration guide for using Smart Proxy with the [penghaiping/amazon-sp-api](https://github.com/penghaiping/amazon-sp-api) Java library.

## Overview

The `amazon-sp-api` Java library is a Swagger/OpenAPI-generated SDK using **OkHttp** as its HTTP client. Each API class (e.g. `SellersApi`, `OrdersApi`) uses an `ApiClient` that provides:

- `addDefaultHeader(name, value)`  -  add headers to all requests
- `setBasePath(url)`  -  override the base URL
- `getHttpClient().interceptors()`  -  OkHttp interceptor chain

Both custom headers and base URL override are **natively supported** without forking or subclassing.

## Sending the `X-SP-Proxy-Merchant-Id` Header

### Option A: `addDefaultHeader()` (Recommended)

The simplest approach  -  add the header to the `ApiClient` after building the API:

```java
SellersApi sellersApi = new SellersApi.Builder()
    .awsAuthenticationCredentials(awsCredentials)
    .lwaAuthorizationCredentials(lwaCredentials)
    .endpoint("https://sellingpartnerapi-eu.amazon.com")
    .build();

// Add the merchant ID header to all requests
sellersApi.getApiClient().addDefaultHeader("X-SP-Proxy-Merchant-Id", "YOUR_SELLER_ID");
```

Default headers are merged into every request via `processHeaderParams()` and survive the AWS SigV4 signing process.

### Option B: OkHttp Interceptor

For dynamic or conditional headers, use an OkHttp interceptor:

```java
import com.squareup.okhttp.Interceptor;
import com.squareup.okhttp.Request;
import com.squareup.okhttp.Response;

SellersApi sellersApi = new SellersApi.Builder()
    .awsAuthenticationCredentials(awsCredentials)
    .lwaAuthorizationCredentials(lwaCredentials)
    .endpoint("https://sellingpartnerapi-eu.amazon.com")
    .build();

sellersApi.getApiClient().getHttpClient().interceptors().add(chain -> {
    Request request = chain.request().newBuilder()
        .addHeader("X-SP-Proxy-Merchant-Id", "YOUR_SELLER_ID")
        .build();
    return chain.proceed(request);
});
```

## Pointing the Library at Smart Proxy

### Option A: Builder `endpoint()` (Recommended)

Each API class has a Builder with an `endpoint()` method:

```java
OrdersApi ordersApi = new OrdersApi.Builder()
    .awsAuthenticationCredentials(awsCredentials)
    .lwaAuthorizationCredentials(lwaCredentials)
    .endpoint("http://localhost:8080")   // <-- Smart Proxy EU
    .build();

ordersApi.getApiClient().addDefaultHeader("X-SP-Proxy-Merchant-Id", "YOUR_SELLER_ID");
```

### Option B: `setBasePath()` After Build

```java
OrdersApi ordersApi = new OrdersApi.Builder()
    .awsAuthenticationCredentials(awsCredentials)
    .lwaAuthorizationCredentials(lwaCredentials)
    .build();

ordersApi.getApiClient().setBasePath("http://localhost:8080");
ordersApi.getApiClient().addDefaultHeader("X-SP-Proxy-Merchant-Id", "YOUR_SELLER_ID");
```

### Region-to-Port Mapping

| SP-API Endpoint | Region | Smart Proxy Port |
|---|---|---|
| `sellingpartnerapi-eu.amazon.com` | EU | `8080` |
| `sellingpartnerapi-na.amazon.com` | NA | `8081` |
| `sellingpartnerapi-fe.amazon.com` | FE | `8082` |

## Full Example

```java
import com.amazon.spapi.api.OrdersApi;
import com.amazon.spapi.client.ApiException;
import com.amazon.spapi.model.orders.GetOrdersResponse;
import com.amazon.spapi.SellingPartnerAPIAA.*;

import java.util.Arrays;
import java.util.Map;

public class SmartProxyExample {

    private static final Map<String, String> PROXY_ENDPOINTS = Map.of(
        "eu", "http://localhost:8080",
        "na", "http://localhost:8081",
        "fe", "http://localhost:8082"
    );

    public static OrdersApi createProxiedOrdersApi(
            AWSAuthenticationCredentials awsCredentials,
            LWAAuthorizationCredentials lwaCredentials,
            String region,
            String merchantId
    ) {
        String proxyEndpoint = PROXY_ENDPOINTS.getOrDefault(region, PROXY_ENDPOINTS.get("eu"));

        OrdersApi ordersApi = new OrdersApi.Builder()
            .awsAuthenticationCredentials(awsCredentials)
            .lwaAuthorizationCredentials(lwaCredentials)
            .endpoint(proxyEndpoint)
            .build();

        ordersApi.getApiClient().addDefaultHeader("X-SP-Proxy-Merchant-Id", merchantId);

        return ordersApi;
    }

    public static void main(String[] args) throws ApiException {
        AWSAuthenticationCredentials awsCredentials = AWSAuthenticationCredentials.builder()
            .accessKeyId("AKIAIOSFODNN7EXAMPLE")
            .secretKey("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
            .region("eu-west-1")
            .build();

        LWAAuthorizationCredentials lwaCredentials = LWAAuthorizationCredentials.builder()
            .clientId("amzn1.application-oa2-client.xxx")
            .clientSecret("your-secret")
            .refreshToken("Atzr|xxx")
            .endpoint("https://api.amazon.com/auth/o2/token")
            .build();

        OrdersApi ordersApi = createProxiedOrdersApi(
            awsCredentials,
            lwaCredentials,
            "eu",
            "YOUR_SELLER_ID"
        );

        GetOrdersResponse response = ordersApi.getOrders(
            Arrays.asList("A1PA6795UKMFR9"),  // marketplaceIds
            "2024-01-01T00:00:00Z",            // createdAfter
            null, null, null, null, null, null, null, null
        );

        System.out.println(response);
    }
}
```

## Helper: Apply Proxy Config to Any API Class

Since every API class follows the same pattern, a helper method keeps things DRY:

```java
import com.amazon.spapi.client.ApiClient;

public class SmartProxyConfig {

    private final String merchantId;
    private final String proxyEndpoint;

    public SmartProxyConfig(String merchantId, String proxyEndpoint) {
        this.merchantId = merchantId;
        this.proxyEndpoint = proxyEndpoint;
    }

    /**
     * Apply Smart Proxy configuration to any API client.
     * Call after building the API instance.
     */
    public void apply(ApiClient apiClient) {
        apiClient.setBasePath(proxyEndpoint);
        apiClient.addDefaultHeader("X-SP-Proxy-Merchant-Id", merchantId);
    }
}

// Usage
SmartProxyConfig proxyConfig = new SmartProxyConfig("YOUR_SELLER_ID", "http://localhost:8080");

OrdersApi ordersApi = new OrdersApi.Builder()
    .awsAuthenticationCredentials(awsCredentials)
    .lwaAuthorizationCredentials(lwaCredentials)
    .build();
proxyConfig.apply(ordersApi.getApiClient());

ReportsApi reportsApi = new ReportsApi.Builder()
    .awsAuthenticationCredentials(awsCredentials)
    .lwaAuthorizationCredentials(lwaCredentials)
    .build();
proxyConfig.apply(reportsApi.getApiClient());
```

## Optional Proxy Headers

```java
ApiClient apiClient = ordersApi.getApiClient();
apiClient.addDefaultHeader("X-SP-Proxy-Merchant-Id", "YOUR_SELLER_ID");
// apiClient.addDefaultHeader("X-SP-Proxy-No-Cache", "true");       // bypass cache
// apiClient.addDefaultHeader("X-SP-Proxy-Cache-TTL", "10m");       // custom TTL
// apiClient.addDefaultHeader("X-SP-Proxy-Priority", "high");       // queue priority
```

See the main [README](../../README.md#request-headers) for all available headers.
