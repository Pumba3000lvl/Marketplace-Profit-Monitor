# Marketplace Profit Monitor

A Grafana data source plugin for querying seller APIs from Wildberries and Ozon. It supports direct API requests and normalized marketplace product metrics, with available commissions, prices, and margin inputs represented without inventing unavailable costs.

## Prerequisites

- Node.js 22 or newer and npm
- Docker Compose (the Makefile uses local Go 1.23.5+ when available, or Docker to build/test the backend)

## Build and run locally

Build the frontend bundle and Linux backend executable:

```sh
npm ci
make build
docker compose up -d
```

Open [http://localhost:3000](http://localhost:3000) and sign in with Grafana's initial local-development credentials, `admin` / `admin`. Docker Compose provisions **Marketplace Profit Monitor (Alerts)** with the stable UID `marketplace-profit-monitor`; open it under **Connections → Data sources** and configure the marketplace credentials you need. The provisioning file contains no credentials. Wildberries API Key and Ozon API Key are stored in Grafana's encrypted `secureJsonData`; Ozon Client-Id is stored in datasource `jsonData`. Grafana decrypts secure settings into `backend.DataSourceInstanceSettings.DecryptedSecureJSONData` when it creates the backend datasource instance; `QueryData` reuses that instance state rather than reading credentials from `PluginContext`. Save the data source before using **Test connection**; it probes only configured marketplaces through the plugin backend. Grafana's **Save & test** check also makes a read-only request to each configured marketplace. The datasource editor's Telegram fields are legacy settings and are not used by the provisioned alert contact points.

### Telegram alert notifications

Grafana OSS provisions two Telegram contact points and routes alerts by their `severity` label: `critical` goes to the critical chat ID, and `warning` goes to the warning chat ID. Both contacts use the same bot token. Alerts without either severity use the warning contact point as the policy's fallback. The five marketplace alert rules remain paused until their source metrics are implemented and verified, so they will not send notifications until you intentionally unpause them.

1. Create a bot with [@BotFather](https://t.me/BotFather), start a private conversation with it using `/start`, or add it to the target group/channel with permission to post. Use separate destinations for critical and warning alerts if desired.
2. Copy `.env.example` to `.env` in the repository root and set `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CRITICAL_CHAT_ID`, and `TELEGRAM_WARNING_CHAT_ID`. Keep `.env` local and private; it is ignored by Git. Do not paste bot tokens into chat, commit them, or put them in provisioning files. If a token is ever shared, revoke it with @BotFather and configure its replacement locally.
3. Recreate Grafana to load the environment and provision the contact points:

   ```sh
   docker compose up -d --force-recreate grafana
   ```

   Empty values are allowed so Grafana can start without Telegram configured, but the contacts will not be usable until all three variables are set. After provisioning, use **Alerting → Contact points** in Grafana to review and explicitly test each contact point. File-provisioned contacts are not editable in the UI; update `.env` and recreate Grafana to change them.

The policy file at `provisioning/alerting/marketplace-telegram.yml` defines the complete notification policy tree. Grafana provisioning replaces the entire tree, including routes created in the UI. Before using it on an instance with existing notification policies, export and merge those policies into this file so they are preserved. File-based alert provisioning is supported for Grafana OSS, not Grafana Cloud.

### Grafana alert rules

Docker Compose mounts the datasource and alerting provisioning directories into Grafana and loads five Grafana-managed alert rules from `provisioning/alerting/marketplace-alerts.yml`. The plugin manifest enables alerting and requires Grafana 11 or newer. The rules evaluate every minute, use labeled per-product numeric series, and explicitly preserve Grafana's `NoData` and `Error` states. They are all provisioned **paused**: current provider output cannot support these financial comparisons without inventing values.

| Rule | Threshold | Status and missing data |
| --- | --- | --- |
| Низкая маржинальность товара | Margin below 10% for 5 minutes | Paused: current collectors do not supply complete costs or net-margin values. |
| Рост комиссии маркетплейса | Commission increase above 5% over 7 days | Paused: there is no retained per-product commission history; Ozon commission data is unavailable. |
| Цена выше цены конкурента | Competitor price difference below -5% | Paused: competitor prices are not collected. |
| Высокая доля расходов на хранение | Storage cost above 15% of revenue | Paused: per-product storage costs and a matching revenue measure are unavailable. |
| Отрицательная чистая маржа товара | Net margin below 0 RUB | Paused: current providers do not supply the complete inputs needed to calculate net margin. |

The first rule is evaluated once a minute and requires the margin threshold to hold for five minutes. Warning severity is used for low-margin, commission, competitor-price, and storage-cost alerts; negative net margin is critical. `queryType: alert` is available in the query editor and provisioning contract. It returns only known numeric values as timestamped series labeled by marketplace and product; missing or unsupported values produce an empty series (Grafana `NoData`), never zero. In particular, the commission-history, competitor-price, and storage-cost/revenue metrics intentionally return no series until their source data and calculations exist.

To enable a rule, first implement and verify its missing data source, run the corresponding **Alert metric** query in Grafana, and confirm that it returns the expected per-product labels and numeric values over the required time window. Then set only that rule's `isPaused` to `false` in the provisioning file and restart Grafana. Keep `noDataState: NoData` and `execErrState: Error`; do not treat either state as a healthy zero. The provisioned Telegram contact points route notifications using each alert's `severity` label.

The query editor exposes marketplace and query-type selectors, marketplace-scoped category filters, Grafana's dashboard time range, and a bounded record limit. Product metric queries use the selected marketplace provider and return product prices, commissions, or profitability frames; the limit is applied to returned records. The current Ozon provider does not supply commission data, so commissions queries identify Ozon results as unavailable rather than fabricating values. The currently exposed category groups do not map to supported category filters in the typed providers, so selecting one returns a clear bad-request error rather than silently ignoring it. Ozon history uses the Grafana time range for daily analytics. Wildberries price history cannot be enumerated from a date range because its API requires product and upload IDs that the query model does not provide; that limitation is returned explicitly. The raw API controls remain available for direct seller API requests: choose a route, HTTP method, relative API path, and optional JSON request body. Paths must start with `/` and are appended to the selected marketplace API host. Successful requests are returned as a table with the route, HTTP status, and raw JSON response. Use the official marketplace API documentation to choose supported paths and request bodies:

- [Wildberries seller API](https://dev.wildberries.ru/)
- [Ozon Seller API](https://docs.ozon.ru/api/seller/)

### Localized query-state messages

`QueryStateMessage` (`src/components/QueryStateMessage.tsx`) provides Russian-language alerts for common query setup, API, and empty-result states. It uses Grafana's `Alert` component, maps HTTP 401 and 429 responses with `queryStateFromHttpStatus`, and shows a retry button only for rate-limit states when `onRetry` is supplied. It renders nothing when `type` is omitted.

Use it in a query editor or a result-state component:

```tsx
import {
  QueryStateMessage,
  QueryStateMessageType,
  queryStateFromHttpStatus,
} from './QueryStateMessage';

// Before sending a request:
{!hasApiKey && <QueryStateMessage type={QueryStateMessageType.ApiKeyNotConfigured} />}
{categories.length === 0 && <QueryStateMessage type={QueryStateMessageType.NoCategoriesSelected} />}

// When handling an HTTP response:
const state = queryStateFromHttpStatus(response.status);
return (
  <QueryStateMessage
    type={state}
    onRetry={state === QueryStateMessageType.TooManyRequests ? onRunQuery : undefined}
  />
);

// For a successful request with no rows:
{rows.length === 0 && <QueryStateMessage type={QueryStateMessageType.NoData} />}
```

The standard states display fixed, safe messages: missing API-key configuration and missing categories are warnings, HTTP 429 is a retryable warning, HTTP 401 is an error, and an empty period is informational. `message` and `title` may override the displayed copy; only pass localized, sanitized text. Do not pass raw backend/API errors, response bodies, request headers, or credential values.

The `wb-tariffs` route targets Wildberries' common API (`common-api.wildberries.ru`), while `wb-prices` targets its prices and discounts API (`discounts-prices-api.wildberries.ru`). The Wildberries developer portal is documentation, not an API host. The `ozon` route targets `api-seller.ozon.ru`.

The plugin's `plugin.json` also declares Grafana data-source proxy routes with these same IDs. The current Go backend sends queries directly to the fixed API hosts using the configured credentials; the proxy routes are available for future browser-side/plugin proxy calls and do not change that backend request path.

## Wildberries Go client

The `pkg/wildberries` package provides a typed seller API client:

```go
client := wildberries.NewClient(apiKey)
commissions, err := client.GetCommissions(ctx, "ru")
products, err := client.GetProducts(ctx, 1000, 0)
```

The client uses a 30-second HTTP timeout, sends the API key in the `Authorization` header, logs only request method/path/status/duration and cache hit/miss type, and retries server (5xx) errors with context-aware exponential backoff. Its thread-safe, process-local in-memory cache uses a 1-hour TTL for commissions, 5 minutes for upload/price history, and 30 minutes for products. Cache keys are isolated by data type, shop ID, canonical query parameters, and a one-way API-key fingerprint. API errors are not cached.

`NewClient(apiKey)` creates a client with its own cache and the default shop scope. Use `NewClientWithCache(apiKey, shopID, cache)` to provide a stable shop ID and a caller-managed cache. Call `client.InvalidateCache()` when manually refreshing data, or `client.SetAPIKey(newKey)` to rotate credentials and invalidate that shop's entries. `client.CacheMetrics()` reports aggregate and per-type hit/miss counts and rates. Close a default client with `client.Close()`; a cache passed to `NewClientWithCache` remains caller-owned and should be closed by its owner. This cache is local to one process and does not coordinate between Grafana backend instances. The generic Grafana query editor sends arbitrary requests directly and does not use this typed-client cache.

The client includes `GetUploadTask` and `GetUploadTaskDetails` for processed price uploads, following the [commission](https://dev.wildberries.ru/openapi/rates#tag/fees/operation/getV1TariffsCommission), [current product prices](https://dev.wildberries.ru/openapi/item-management#tag/pricesAndDiscounts/operation/getV2ListGoodsFilter), and [processed upload history](https://dev.wildberries.ru/openapi/item-management#tag/pricesAndDiscounts/operation/getV2HistoryTasks) operations.

`GetPriceHistory(ctx, nmID, dateFrom, dateTo)` currently returns `ErrUploadTaskEnumerationUnsupported`. Wildberries' processed-history endpoints (`GET /api/v2/history/tasks` and `GET /api/v2/history/goods/task`) both require an `uploadID`, and the API does not expose an operation to enumerate those IDs. `GetPriceHistoryForUploads` can retrieve and filter price points when the caller already has the upload IDs. Those points represent changes submitted through the Wildberries API only; changes made manually in the seller cabinet are not available through these endpoints and are not included.

## Ozon Seller API Go client

The `pkg/ozon` package provides a typed client for product listing, current prices, and analytics:

```go
package main

import (
    "context"
    "fmt"
    "os"
    "log"
    "os"

    "github.com/pumba3000lvl/marketplace-profit-monitor/pkg/ozon"
)

func main() {
    ctx := context.Background()
    client := ozon.NewClient(os.Getenv("OZON_CLIENT_ID"), os.Getenv("OZON_API_KEY"))

    products, err := client.GetProductList(ctx, 1000, "")
    if err != nil {
        log.Fatal(err)
    }
    productIDs := make([]string, 0, len(products))
    for _, product := range products {
        productIDs = append(productIDs, product.ProductID)
    }
    prices, err := client.GetPrices(ctx, productIDs)
    if err != nil {
        log.Fatal(err)
    }
    analytics, err := client.GetAnalytics(ctx, "2026-01-01", "2026-01-31", []string{"revenue", "ordered_units"})
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("products=%d prices=%d analytics rows=%d\n", len(products), len(prices), len(analytics))
}
```

Each request is a JSON `POST` with the `Client-Id` and `Api-Key` headers and a 30-second timeout. The client logs the HTTP method, path, status, duration, and Ozon request ID without logging credentials or request bodies. `APIError` exposes the HTTP status, Ozon error code/message, and request ID.

The methods use `POST /v3/product/list`, `POST /v5/product/info/prices`, and `POST /v1/analytics/data`, respectively. `GetProductList` follows Ozon's `last_id` cursor through all pages, starting at the supplied cursor; it returns the accumulated products rather than one page. `GetPrices` deduplicates product IDs, batches selections to the API page-size limit, and follows the prices cursor. Price amounts are exposed as decimal strings in `PriceItem` to preserve Ozon's exact values. `GetAnalytics` retrieves all pages and groups results by day, the API-required default dimension because its signature accepts metrics but no dimension.

See the [Ozon Seller API documentation](https://docs.ozon.ru/api/seller/) for available analytics metric names and endpoint limits.

## Unified marketplace metrics

The `pkg/marketplace` package adapts the typed Wildberries and Ozon clients to one `ProductMetrics` shape. Each row retains its marketplace-specific product ID (`nmID` for Wildberries, Ozon `offer_id`); Ozon's internal numeric `product_id` is also preserved as `MarketplaceProductID` for joining to price responses. Rows include seller SKU, variant identity when applicable, RUB price, available commission values, and retrieval timestamp. `Collect` reports marketplace status independently and keeps rows returned by a provider even when a later request from that same provider fails. `Merge` groups rows only by a trimmed, case-folded seller SKU; it never matches by name or assumes marketplace IDs are shared. Grouped offers remain separate, so prices and costs are not overwritten.

```go
package main

import (
    "context"
    "fmt"

    "github.com/pumba3000lvl/marketplace-profit-monitor/pkg/marketplace"
    "github.com/pumba3000lvl/marketplace-profit-monitor/pkg/ozon"
    "github.com/pumba3000lvl/marketplace-profit-monitor/pkg/wildberries"
)

func main() {
    ctx := context.Background()
    wbClient := wildberries.NewClient(os.Getenv("WILDBERRIES_TOKEN"))
    defer wbClient.Close()
    ozonClient := ozon.NewClient(os.Getenv("OZON_CLIENT_ID"), os.Getenv("OZON_API_KEY"))

    result, err := marketplace.Collect(ctx,
        marketplace.NewWildberriesProvider(wbClient),
        marketplace.NewOzonProvider(ozonClient),
    )
    if err != nil { // Collection-level cancellation; partial results are still in result.
        fmt.Printf("collection stopped: %v\n", err)
    }
    for _, status := range result.Providers {
        fmt.Printf("%s: %s (%d products): %s\n",
            status.Marketplace, status.State, status.ProductCount, status.Error)
    }
    for _, group := range result.Groups {
        fmt.Printf("seller SKU %q has %d marketplace offers\n", group.SellerSKU, len(group.Offers))
    }
}
```

Unavailable amounts are `nil`, not zero. Current clients expose Wildberries' category commission rate and price, plus Ozon's decimal-string price; they do not provide a product name, unit cost, or per-product logistics/storage costs. Accordingly, names and unavailable costs remain empty/nil, and net margin is not fabricated. Wildberries commission amount is derived only when its product subject can be matched to the commission report; Ozon commission and fulfillment costs remain unavailable. Net margin is `current price - commission - logistics - storage - cost price`; to calculate it, first populate all five values in RUB, then call `marketplace.CalculateNetMargin`. The function rejects missing inputs and uses kopecks for monetary arithmetic. Non-RUB prices are left unavailable.

Merge warnings identify offers without a seller SKU (kept as separate groups) and multiple distinct product IDs sharing a seller SKU within one marketplace (all are retained). Wildberries sizes are separate offers with the same `nmID` and distinct variant IDs; they are not reported as duplicate products.

In Grafana's query editor, choose **All marketplaces — product metrics** for this normalized collection. It returns flat product metrics, a grouped frame with a JSON array of the marketplace-specific offers for each SKU, per-marketplace status, and merge warnings; existing raw API route queries are unchanged. Configure the credentials for each marketplace you want to include. A missing credential or marketplace outage appears in the status frame while data from the other provider remains available.

### Calculating net margin

`CalculateNetMargin` accepts monetary amounts as integer kopecks and returns a net margin, display percentage, and dashboard-ready expense breakdown. Pass commission as integer basis points (`100` = 1%, `10_000` = 100%). The optional ad cost defaults to zero; a zero cost price omits cost of goods. Commission is rounded to the nearest kopeck, with half-kopeck amounts rounded up. The margin percentage is calculated from the final integer amounts as `float64` for display only.

```go
result, err := wildberries.CalculateNetMargin(wildberries.MarginInput{
    SalePriceKopecks:      10_000, // 100 RUB
    CommissionBasisPoints: 1_500,  // 15%
    LogisticsCostKopecks:  500,
    StorageCostKopecks:    100,
    CostPriceKopecks:      3_000,
    AdCostKopecks:         200,
})
if err != nil {
    return err
}
fmt.Printf("margin: %d kopecks (%.2f%%), expenses: %d kopecks\n",
    result.NetMarginKopecks, result.NetMarginPercent, result.TotalExpensesKopecks)
```

Grafana data is stored in the `grafana-data` named volume. To stop Grafana:

```sh
docker compose down
```

To also remove the persisted Grafana data, run `docker compose down -v`.

## Optional Redis

Start the optional Redis service with:

```sh
docker compose --profile cache up -d
```

Redis uses the `redis-data` named volume. The plugin does not connect to Redis yet; enabling this profile only starts Redis for local development.

## Development

After changing frontend code, run `npm run build` and restart Grafana if you change `plugin.json`. The build preserves the backend binary. After changing Go code, run `make backend` and restart Grafana. To run the Go tests, use `make test`.

`make clean` removes generated `dist/` artifacts. Do not expose this unsigned-plugin development Grafana instance to an untrusted network.
