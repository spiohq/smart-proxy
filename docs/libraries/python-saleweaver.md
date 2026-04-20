# Python  -  saleweaver/python-amazon-sp-api

Integration guide for using Smart Proxy with the [saleweaver/python-amazon-sp-api](https://github.com/saleweaver/python-amazon-sp-api) Python library.

## Overview

The `python-amazon-sp-api` library uses [httpx](https://www.python-httpx.org/) as its HTTP transport. Each API class (e.g. `Orders`, `Reports`) extends a base `Client` class that builds headers via a `headers` property and resolves the endpoint from a `Marketplaces` enum.

The library does **not** have built-in parameters for custom headers or a custom base URL, but both can be added by subclassing or by reassigning attributes after instantiation.

## Sending the `X-SP-Proxy-Merchant-Id` Header

### Option A: Subclass with Custom Headers (Recommended)

Override the `headers` property on the API class you use:

```python
from sp_api.api import Orders

class ProxiedOrders(Orders):
    def __init__(self, *args, merchant_id: str = "", **kwargs):
        self._merchant_id = merchant_id
        super().__init__(*args, **kwargs)

    @property
    def headers(self):
        h = super().headers
        h["X-SP-Proxy-Merchant-Id"] = self._merchant_id
        return h

orders = ProxiedOrders(
    marketplace=Marketplaces.DE,
    merchant_id="YOUR_SELLER_ID",
)
response = orders.get_orders(CreatedAfter="2024-01-01")
```

### Option B: Reassign After Instantiation

If you don't want to subclass, you can monkey-patch the `headers` property on the class:

```python
from sp_api.api import Orders
from sp_api.base import Client

MERCHANT_ID = "YOUR_SELLER_ID"

_original_headers = Client.headers

@property
def _custom_headers(self):
    h = _original_headers.fget(self)
    h["X-SP-Proxy-Merchant-Id"] = MERCHANT_ID
    return h

Client.headers = _custom_headers

# All API classes now send the header automatically
orders = Orders(marketplace=Marketplaces.DE)
response = orders.get_orders(CreatedAfter="2024-01-01")
```

> **Note:** This patches `Client` globally  -  all API classes (`Orders`, `Reports`, `Catalog`, etc.) will include the header.

### Option C: Reusable Mixin

If you use multiple API classes, a mixin avoids repeating the override:

```python
from sp_api.base import Client

class ProxyMixin:
    """Mixin that adds Smart Proxy headers to any SP-API client."""

    _merchant_id: str = ""

    @property
    def headers(self):
        h = super().headers
        if self._merchant_id:
            h["X-SP-Proxy-Merchant-Id"] = self._merchant_id
        return h

# Apply to any API class
from sp_api.api import Orders, Reports, CatalogItems

class ProxiedOrders(ProxyMixin, Orders): pass
class ProxiedReports(ProxyMixin, Reports): pass
class ProxiedCatalog(ProxyMixin, CatalogItems): pass

# Usage
orders = ProxiedOrders(marketplace=Marketplaces.DE)
orders._merchant_id = "YOUR_SELLER_ID"
```

## Pointing the Library at Smart Proxy

The endpoint is set from the `Marketplaces` enum and stored as `self.endpoint` on the client. Override it after instantiation or in a subclass.

### Override After Instantiation

```python
from sp_api.api import Orders
from sp_api.base import Marketplaces

orders = Orders(marketplace=Marketplaces.DE)
orders.endpoint = "http://localhost:8080"

response = orders.get_orders(CreatedAfter="2024-01-01")
```

### Override in Subclass

```python
from sp_api.api import Orders

PROXY_PORTS = {
    "EU": 8080,
    "NA": 8081,
    "FE": 8082,
}

class ProxiedOrders(Orders):
    def __init__(self, *args, proxy_host: str = "http://localhost", **kwargs):
        super().__init__(*args, **kwargs)
        region = self.marketplace.region  # "EU", "NA", or "FE"
        port = PROXY_PORTS.get(region, 8080)
        self.endpoint = f"{proxy_host}:{port}"
```

### Region-to-Port Mapping

| Marketplace             | Region | Smart Proxy Port |
|-------------------------|--------|------------------|
| `Marketplaces.DE`, `.FR`, `.ES`, `.IT`, `.UK`, etc. | EU | `8080` |
| `Marketplaces.US`, `.CA`, `.MX`, `.BR`              | NA | `8081` |
| `Marketplaces.JP`, `.AU`, `.SG`, `.IN`              | FE | `8082` |

## Full Example

```python
from sp_api.api import Orders, Reports
from sp_api.base import Marketplaces, Client

PROXY_HOST = "http://localhost"
MERCHANT_ID = "YOUR_SELLER_ID"

PROXY_PORTS = {
    "EU": 8080,
    "NA": 8081,
    "FE": 8082,
}


class ProxyMixin:
    """Adds Smart Proxy endpoint and merchant header to any SP-API client."""

    def __init__(self, *args, merchant_id: str = "", proxy_host: str = PROXY_HOST, **kwargs):
        self._merchant_id = merchant_id
        super().__init__(*args, **kwargs)
        region = self.marketplace.region
        port = PROXY_PORTS.get(region, 8080)
        self.endpoint = f"{proxy_host}:{port}"

    @property
    def headers(self):
        h = super().headers
        h["X-SP-Proxy-Merchant-Id"] = self._merchant_id
        return h


class ProxiedOrders(ProxyMixin, Orders): pass
class ProxiedReports(ProxyMixin, Reports): pass


# Usage
orders = ProxiedOrders(
    marketplace=Marketplaces.DE,
    merchant_id=MERCHANT_ID,
)

response = orders.get_orders(CreatedAfter="2024-01-01")
print(response.payload)
```

## Optional Proxy Headers

Add more Smart Proxy headers in the `headers` property:

```python
@property
def headers(self):
    h = super().headers
    h["X-SP-Proxy-Merchant-Id"] = self._merchant_id
    # h["X-SP-Proxy-No-Cache"]  = "true"       # bypass cache
    # h["X-SP-Proxy-Cache-TTL"] = "10m"         # custom TTL
    # h["X-SP-Proxy-Priority"]  = "high"        # queue priority
    return h
```

See the main [README](../../README.md#request-headers) for all available headers.
