# Marketplace Profit Monitor

A Grafana data source plugin scaffold for querying seller APIs from Wildberries and Ozon. It currently sends one API request per query and displays the response as a table; turning marketplace payloads into normalized sales, costs, and profit metrics is future work.

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

Open [http://localhost:3000](http://localhost:3000) and sign in with Grafana's initial local-development credentials, `admin` / `admin`. Add **Marketplace Profit Monitor** under **Connections → Data sources**, configure the API credentials you need, and select **Save & test**. The health check verifies that at least one supported credential is configured; it does not call a marketplace API.

The query editor lets you select a route, HTTP method, relative API path, and optional JSON request body. Paths must start with `/` and are appended to the selected marketplace API host. Successful requests are returned as a table with the route, HTTP status, and raw JSON response. Use the official marketplace API documentation to choose supported paths and request bodies:

- [Wildberries seller API](https://dev.wildberries.ru/)
- [Ozon Seller API](https://docs.ozon.ru/api/seller/)

The `wb-tariffs` route targets Wildberries' common API (`common-api.wildberries.ru`), while `wb-prices` targets its prices and discounts API (`discounts-prices-api.wildberries.ru`). The Wildberries developer portal is documentation, not an API host. The `ozon` route targets `api-seller.ozon.ru`.

The plugin's `plugin.json` also declares Grafana data-source proxy routes with these same IDs. The current Go backend sends queries directly to the fixed API hosts using the configured credentials; the proxy routes are available for future browser-side/plugin proxy calls and do not change that backend request path.

## Wildberries Go client

The `pkg/wildberries` package provides a typed seller API client:

```go
client := wildberries.NewClient(apiKey)
commissions, err := client.GetCommissions(ctx, "ru")
products, err := client.GetProducts(ctx, 1000, 0)
```

It uses a 30-second HTTP timeout, sends the API key in the `Authorization` header, logs only request method/path/status/duration, and retries server (5xx) errors with context-aware exponential backoff. The client includes `GetUploadTask` and `GetUploadTaskDetails` for processed price uploads, following the [commission](https://dev.wildberries.ru/openapi/rates#tag/fees/operation/getV1TariffsCommission), [current product prices](https://dev.wildberries.ru/openapi/item-management#tag/pricesAndDiscounts/operation/getV2ListGoodsFilter), and [processed upload history](https://dev.wildberries.ru/openapi/item-management#tag/pricesAndDiscounts/operation/getV2HistoryTasks) operations.

`GetPriceHistory(ctx, nmID, dateFrom, dateTo)` currently returns `ErrUploadTaskEnumerationUnsupported`. Wildberries' processed-history endpoints (`GET /api/v2/history/tasks` and `GET /api/v2/history/goods/task`) both require an `uploadID`, and the API does not expose an operation to enumerate those IDs. `GetPriceHistoryForUploads` can retrieve and filter price points when the caller already has the upload IDs. Those points represent changes submitted through the Wildberries API only; changes made manually in the seller cabinet are not available through these endpoints and are not included.

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
