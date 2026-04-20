# Ruby  -  patterninc/muffin_man

Integration guide for using Smart Proxy with the [patterninc/muffin_man](https://github.com/patterninc/muffin_man) Ruby gem.

## Overview

The `muffin_man` gem uses **Typhoeus** as its HTTP client. All API classes (e.g. `MuffinMan::Orders::V0`) inherit from `SpApiClient`, which builds headers and resolves the endpoint internally.

The gem has **no built-in support** for custom headers or base URL overrides. Both the `headers` method and the endpoint URL are hardcoded in `SpApiClient`. The recommended approach is to monkey-patch the relevant methods.

## Sending the `X-SP-Proxy-Merchant-Id` Header

### Option A: Monkey-Patch `headers` (Recommended)

Override the private `headers` method on `SpApiClient` to inject the custom header:

```ruby
require "muffin_man"

module SmartProxyHeaders
  MERCHANT_ID = "YOUR_SELLER_ID"

  def headers
    super.merge(
      "X-SP-Proxy-Merchant-Id" => MERCHANT_ID
    )
  end
end

MuffinMan::SpApiClient.prepend(SmartProxyHeaders)
```

After this, all API calls across all modules (`Orders`, `Reports`, `Catalog`, etc.) will include the header.

### Option B: Per-Instance Monkey-Patch

If you need different merchant IDs per instance:

```ruby
require "muffin_man"

class MuffinMan::SpApiClient
  attr_accessor :proxy_merchant_id

  alias_method :original_headers, :headers

  private

  def headers
    h = original_headers
    h["X-SP-Proxy-Merchant-Id"] = proxy_merchant_id if proxy_merchant_id
    h
  end
end

# Usage
orders = MuffinMan::Orders::V0.new(credentials)
orders.proxy_merchant_id = "YOUR_SELLER_ID"

response = orders.get_orders(["A1PA6795UKMFR9"], CreatedAfter: "2024-01-01T00:00:00Z")
```

### Option C: Subclass SpApiClient

For a cleaner separation, subclass the API class you need:

```ruby
class ProxiedOrders < MuffinMan::Orders::V0
  def initialize(credentials, sandbox = false, merchant_id:)
    @merchant_id = merchant_id
    super(credentials, sandbox)
  end

  private

  def headers
    super.merge("X-SP-Proxy-Merchant-Id" => @merchant_id)
  end
end

orders = ProxiedOrders.new(credentials, merchant_id: "YOUR_SELLER_ID")
response = orders.get_orders(["A1PA6795UKMFR9"], CreatedAfter: "2024-01-01T00:00:00Z")
```

> **Note:** Subclassing must be done per API class (`Orders::V0`, `Reports::V0`, etc.). Use the monkey-patch approach if you want a single global change.

## Pointing the Library at Smart Proxy

The endpoint is hardcoded in `SpApiClient#sp_api_url` as `https://sellingpartnerapi-{region}.amazon.com`. Override it with a prepend:

```ruby
module SmartProxyEndpoint
  PROXY_PORTS = {
    "na" => 8081,
    "eu" => 8080,
    "fe" => 8082,
  }.freeze

  PROXY_HOST = "http://localhost"

  def sp_api_url
    port = PROXY_PORTS.fetch(region, 8080)
    "#{PROXY_HOST}:#{port}"
  end
end

MuffinMan::SpApiClient.prepend(SmartProxyEndpoint)
```

### Region-to-Port Mapping

| `credentials[:region]` | SP-API Endpoint | Smart Proxy Port |
|---|---|---|
| `"eu"` | `sellingpartnerapi-eu.amazon.com` | `8080` |
| `"na"` | `sellingpartnerapi-na.amazon.com` | `8081` |
| `"fe"` | `sellingpartnerapi-fe.amazon.com` | `8082` |

## Full Example

Combine both patches into a single initializer:

```ruby
# config/initializers/smart_proxy.rb  (or require at boot)

require "muffin_man"

module SmartProxy
  PROXY_HOST = ENV.fetch("SP_PROXY_HOST", "http://localhost")
  MERCHANT_ID = ENV.fetch("SP_PROXY_MERCHANT_ID", "")

  PROXY_PORTS = {
    "na" => 8081,
    "eu" => 8080,
    "fe" => 8082,
  }.freeze

  module Patch
    # Override endpoint to point at Smart Proxy
    def sp_api_url
      port = SmartProxy::PROXY_PORTS.fetch(region, 8080)
      "#{SmartProxy::PROXY_HOST}:#{port}"
    end

    # Inject merchant ID header
    def headers
      h = super
      h["X-SP-Proxy-Merchant-Id"] = SmartProxy::MERCHANT_ID unless SmartProxy::MERCHANT_ID.empty?
      h
    end
  end
end

MuffinMan::SpApiClient.prepend(SmartProxy::Patch)
```

Usage:

```ruby
credentials = {
  refresh_token: "Atzr|xxx",
  client_id: "amzn1.application-oa2-client.xxx",
  client_secret: "your-secret",
  region: "eu",
}

MuffinMan.configure do |config|
  config.save_access_token = ->(token_key, token) { Rails.cache.write(token_key, token) }
  config.get_access_token = ->(token_key) { Rails.cache.read(token_key) }
end

# All calls now go through Smart Proxy with the merchant ID header
orders = MuffinMan::Orders::V0.new(credentials)
response = orders.get_orders(["A1PA6795UKMFR9"], CreatedAfter: "2024-01-01T00:00:00Z")

reports = MuffinMan::Reports::V0.new(credentials)
response = reports.get_reports
```

## Optional Proxy Headers

Add more Smart Proxy headers in the patch:

```ruby
def headers
  h = super
  h["X-SP-Proxy-Merchant-Id"] = SmartProxy::MERCHANT_ID unless SmartProxy::MERCHANT_ID.empty?
  # h["X-SP-Proxy-No-Cache"]  = "true"       # bypass cache
  # h["X-SP-Proxy-Cache-TTL"] = "10m"         # custom TTL
  # h["X-SP-Proxy-Priority"]  = "high"        # queue priority
  h
end
```

See the main [README](../../README.md#request-headers) for all available headers.
