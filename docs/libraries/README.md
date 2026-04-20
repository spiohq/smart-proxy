# SP-API Library Integration Guides

Step-by-step guides for integrating Smart Proxy with popular Amazon Selling Partner API client libraries. Each guide covers:

- How to send the `X-SP-Proxy-Merchant-Id` header for optimal caching and rate-limit behavior
- How to point the library at Smart Proxy instead of Amazon's endpoints
- Full working examples with region-to-port mapping

## Libraries

| Language | Library | Custom Headers | Base URL Override | Difficulty |
|---|---|---|---|---|
| PHP | [jlevers/selling-partner-api](php-jlevers.md) | Middleware / Subclass | `resolveBaseUrl()` override | Easy |
| PHP | [amazon-php/sp-api-sdk](php-amazon-php.md) (+ plentymarkets fork) | PSR-18 Client wrapper | PSR-18 Client wrapper | Easy |
| Python | [saleweaver/python-amazon-sp-api](python-saleweaver.md) | `headers` property override | `self.endpoint` reassign | Medium |
| Node.js | [amz-tools/amazon-sp-api](node-amz-tools.md) | `headers` in `callAPI()` (built-in) | Endpoint patch required | Easy |
| TypeScript | [ScaleLeap/selling-partner-api-sdk](ts-scaleleap.md) | `baseOptions.headers` (built-in) | `basePath` (built-in) | Easy |
| Java | [penghaiping/amazon-sp-api](java-penghaiping.md) | `addDefaultHeader()` (built-in) | `endpoint()` / `setBasePath()` (built-in) | Easy |
| C# | [abuzuhri/Amazon-SP-API-CSharp](csharp-abuzuhri.md) | Fork required | Fork required | Hard |
| Ruby | [patterninc/muffin_man](ruby-muffin-man.md) | Monkey-patch `headers` | Monkey-patch `sp_api_url` | Medium |
| Go | [renabled/amzn-sp-api-go](go-renabled.md) | `http.RoundTripper` wrapper | Fork required (unexported field) | Hard |

### Difficulty Legend

- **Easy**  -  Built-in support or simple config change, no forking needed
- **Medium**  -  Requires subclassing, monkey-patching, or a wrapper, but no fork
- **Hard**  -  Requires maintaining a lightweight fork of the library

## Quick Reference

Regardless of library, the core integration is always the same:

1. **Set the base URL** to your Smart Proxy instance (EU → `:8080`, NA → `:8081`, FE → `:8082`)
2. **Send `X-SP-Proxy-Merchant-Id`** with every request for stable cache keys and rate-limit buckets

See the main [README](../../README.md#merchant-id--important-for-caching--rate-limits) for why the merchant ID header matters.
