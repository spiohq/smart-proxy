# C#  -  abuzuhri/Amazon-SP-API-CSharp

Integration guide for using Smart Proxy with the [abuzuhri/Amazon-SP-API-CSharp](https://github.com/abuzuhri/Amazon-SP-API-CSharp) (FikaAmazonAPI) library.

## Overview

The `Amazon-SP-API-CSharp` library uses a custom RestSharp wrapper over `System.Net.Http.HttpClient`. All API service classes (e.g. `OrderService`, `ReportService`) inherit from `RequestService`, which builds headers and resolves the base URL from `AmazonCredential`.

The library does **not** have built-in support for custom headers or base URL overrides. Both require subclassing `RequestService` or modifying the `Region` / `AmazonCredential` objects.

## Sending the `X-SP-Proxy-Merchant-Id` Header

### Option A: Subclass the Service (Recommended)

Each API service (e.g. `OrderService`) inherits from `RequestService`, which exposes `Request.AddOrUpdateHeader()`. Override `ExecuteRequestTry` to inject the header before every request:

```csharp
using FikaAmazonAPI.Services;

public class ProxiedOrderService : OrderService
{
    private readonly string _merchantId;

    public ProxiedOrderService(AmazonCredential credential, string merchantId)
        : base(credential)
    {
        _merchantId = merchantId;
    }

    protected override async Task<T> ExecuteRequestTry<T>(
        RateLimitType rateLimitType = RateLimitType.UNSET,
        CancellationToken cancellationToken = default)
    {
        // Add the merchant ID header before the request is sent
        Request.AddOrUpdateHeader("X-SP-Proxy-Merchant-Id", _merchantId);
        return await base.ExecuteRequestTry<T>(rateLimitType, cancellationToken);
    }
}
```

> **Caveat:** You would need to do this for each service class you use (`OrderService`, `ReportService`, `CatalogItemService`, etc.). See Option C for a cleaner approach.

### Option B: Fork and Patch `RequestService` Directly

If you're comfortable maintaining a fork, the cleanest change is in `RequestService.ExecuteRequestTry()`:

```csharp
// In Source/FikaAmazonAPI/Services/RequestService.cs, inside ExecuteRequestTry():

RestHeader();
AddAccessToken();
AddShippingBusinessId();

// Add this:
if (!string.IsNullOrEmpty(AmazonCredential.ProxyMerchantId))
    Request.AddOrUpdateHeader("X-SP-Proxy-Merchant-Id", AmazonCredential.ProxyMerchantId);
```

And add a property to `AmazonCredential`:

```csharp
// In Source/FikaAmazonAPI/AmazonCredential.cs
public string ProxyMerchantId { get; set; }
```

Usage:

```csharp
var credentials = new AmazonCredential
{
    ClientId = "amzn1.application-oa2-client.xxx",
    ClientSecret = "your-secret",
    RefreshToken = "Atzr|xxx",
    MarketPlace = MarketPlace.Germany,
    ProxyMerchantId = "YOUR_SELLER_ID",   // <-- new
};

var connection = new AmazonConnection(credentials);
var orders = await connection.Orders.GetOrdersAsync(/* ... */);
```

### Option C: Generic Wrapper via `AmazonConnection`

Wrap `AmazonConnection` to inject headers across all services using reflection:

```csharp
public class ProxiedAmazonConnection
{
    private readonly AmazonConnection _connection;
    private readonly string _merchantId;

    public ProxiedAmazonConnection(AmazonCredential credential, string merchantId)
    {
        _connection = new AmazonConnection(credential);
        _merchantId = merchantId;
    }

    public AmazonConnection Connection => _connection;

    /// <summary>
    /// Call this before each API call to inject the header.
    /// </summary>
    public void InjectHeader(RequestService service)
    {
        // RequestService.Request is the current RestRequest
        service.Request?.AddOrUpdateHeader("X-SP-Proxy-Merchant-Id", _merchantId);
    }
}
```

> **Note:** This is less elegant because `Request` is only created when `CreateRequest()` is called internally. Option B (fork) is the most reliable approach.

## Pointing the Library at Smart Proxy

The base URL is resolved from `AmazonCredential.MarketPlace.Region.HostUrl`, which is hardcoded per region. There are two ways to override it.

### Option A: Modify the Region Object

The `Region` class stores `HostUrl` as a readonly field. Create a custom `Region` with Smart Proxy's URL:

```csharp
using FikaAmazonAPI.Utils;

// Use reflection to replace the HostUrl, or create a patched Region
var field = typeof(Region).GetField("HostUrl");
// Note: HostUrl is readonly, so this requires a fork or unsafe reflection
```

### Option B: Fork and Patch `RequestService.ApiBaseUrl` (Recommended)

Override the `ApiBaseUrl` property in `RequestService`:

```csharp
// In Source/FikaAmazonAPI/Services/RequestService.cs

protected string ApiBaseUrl
{
    get
    {
        // Check for custom proxy endpoint first
        if (!string.IsNullOrEmpty(AmazonCredential.ProxyBaseUrl))
            return AmazonCredential.ProxyBaseUrl;

        return AmazonCredential.Environment == Environments.Sandbox
            ? AmazonSandboxUrl
            : AmazonProductionUrl;
    }
}
```

Add the property to `AmazonCredential`:

```csharp
// In Source/FikaAmazonAPI/AmazonCredential.cs
public string ProxyBaseUrl { get; set; }
```

Usage:

```csharp
var credentials = new AmazonCredential
{
    ClientId = "amzn1.application-oa2-client.xxx",
    ClientSecret = "your-secret",
    RefreshToken = "Atzr|xxx",
    MarketPlace = MarketPlace.Germany,
    ProxyMerchantId = "YOUR_SELLER_ID",
    ProxyBaseUrl = "http://localhost:8080",     // <-- Smart Proxy EU
};

var connection = new AmazonConnection(credentials);
```

### Region-to-Port Mapping

| MarketPlace                                     | Region    | Smart Proxy Port |
|-------------------------------------------------|-----------|------------------|
| `Germany`, `France`, `Spain`, `Italy`, `UK`, etc. | Europe    | `8080`           |
| `UnitedStates`, `Canada`, `Mexico`, `Brazil`     | NA        | `8081`           |
| `Japan`, `Australia`, `Singapore`, `India`       | Far East  | `8082`           |

## Full Example (with Fork)

After adding `ProxyMerchantId` and `ProxyBaseUrl` to `AmazonCredential` and patching `RequestService` as described above:

```csharp
using FikaAmazonAPI;
using FikaAmazonAPI.Utils;

var credentials = new AmazonCredential
{
    ClientId = "amzn1.application-oa2-client.xxx",
    ClientSecret = "your-secret",
    RefreshToken = "Atzr|xxx",
    MarketPlace = MarketPlace.Germany,

    // Smart Proxy settings
    ProxyMerchantId = "YOUR_SELLER_ID",
    ProxyBaseUrl = "http://localhost:8080",
};

var connection = new AmazonConnection(credentials);

// All requests now go through Smart Proxy with the merchant ID header
var orders = await connection.Orders.GetOrdersAsync(new()
{
    CreatedAfter = DateTime.UtcNow.AddDays(-7),
    MarketplaceIds = new List<string> { MarketPlace.Germany.Id },
});

foreach (var order in orders)
{
    Console.WriteLine($"{order.AmazonOrderId}  -  {order.OrderStatus}");
}
```

## Optional Proxy Headers

Add more Smart Proxy headers in the same `ExecuteRequestTry` patch:

```csharp
Request.AddOrUpdateHeader("X-SP-Proxy-Merchant-Id", AmazonCredential.ProxyMerchantId);
// Request.AddOrUpdateHeader("X-SP-Proxy-No-Cache", "true");       // bypass cache
// Request.AddOrUpdateHeader("X-SP-Proxy-Cache-TTL", "10m");       // custom TTL
// Request.AddOrUpdateHeader("X-SP-Proxy-Priority", "high");       // queue priority
```

See the main [README](../../README.md#request-headers) for all available headers.

## Note on Approach

Unlike the PHP and Python SP-API libraries, this C# library does not expose easy hooks for custom headers or base URL overrides. **The recommended approach is to maintain a lightweight fork** with the two small changes to `AmazonCredential` and `RequestService` described above. This keeps the changes minimal and easy to rebase when the upstream library updates.
