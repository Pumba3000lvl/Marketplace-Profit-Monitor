package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/pumba3000lvl/marketplace-profit-monitor/pkg/marketplace"
	"github.com/pumba3000lvl/marketplace-profit-monitor/pkg/ozon"
	"github.com/pumba3000lvl/marketplace-profit-monitor/pkg/wildberries"
)

const maxResponseBytes = 5 << 20
const maxRequestBytes = 1 << 20

type marketplaceRoute struct {
	host string
}

var marketplaceRoutes = map[string]marketplaceRoute{
	"wb-tariffs": {host: "https://common-api.wildberries.ru"},
	"wb-prices":  {host: "https://discounts-prices-api.wildberries.ru"},
	"ozon":       {host: "https://api-seller.ozon.ru"},
}

var (
	_ backend.QueryDataHandler   = (*Datasource)(nil)
	_ backend.CheckHealthHandler = (*Datasource)(nil)
)

type credentials struct {
	wildberriesToken string
	ozonClientID     string
	ozonAPIKey       string
}

type Datasource struct {
	client      *http.Client
	credentials credentials
}

type queryModel struct {
	Marketplace string `json:"marketplace"`
	Path        string `json:"path"`
	Method      string `json:"method"`
	Body        string `json:"body"`
}

func NewDatasource(_ context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	return &Datasource{
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		credentials: credentials{
			wildberriesToken: settings.DecryptedSecureJSONData["wildberriesToken"],
			ozonClientID:     settings.DecryptedSecureJSONData["ozonClientId"],
			ozonAPIKey:       settings.DecryptedSecureJSONData["ozonApiKey"],
		},
	}, nil
}

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	for _, query := range req.Queries {
		if isMetricsQuery(query) {
			response.Responses[query.RefID] = d.queryMetrics(ctx, query)
		} else {
			response.Responses[query.RefID] = d.query(ctx, query)
		}
	}
	return response, nil
}

func isMetricsQuery(query backend.DataQuery) bool {
	var model queryModel
	return json.Unmarshal(query.JSON, &model) == nil && model.Marketplace == "metrics"
}

func (d *Datasource) queryMetrics(ctx context.Context, query backend.DataQuery) backend.DataResponse {
	wildberriesClient := wildberries.NewClient(d.credentials.wildberriesToken)
	result, err := marketplace.Collect(
		ctx,
		marketplace.NewWildberriesProvider(wildberriesClient),
		marketplace.NewOzonProvider(ozon.NewClient(d.credentials.ozonClientID, d.credentials.ozonAPIKey)),
	)
	if closeErr := wildberriesClient.Close(); closeErr != nil {
		err = errors.Join(err, fmt.Errorf("close Wildberries client: %w", closeErr))
	}

	return metricsDataResponse(query, result, err)
}

func metricsDataResponse(query backend.DataQuery, result marketplace.CollectionResult, err error) backend.DataResponse {
	productFrame := data.NewFrame(
		"product-metrics",
		data.NewField("marketplace", nil, make([]string, 0, len(result.Products))),
		data.NewField("productId", nil, make([]string, 0, len(result.Products))),
		data.NewField("marketplaceProductId", nil, make([]string, 0, len(result.Products))),
		data.NewField("sellerSku", nil, make([]string, 0, len(result.Products))),
		data.NewField("normalizedSellerSku", nil, make([]string, 0, len(result.Products))),
		data.NewField("name", nil, make([]string, 0, len(result.Products))),
		data.NewField("variantId", nil, make([]string, 0, len(result.Products))),
		data.NewField("variantName", nil, make([]string, 0, len(result.Products))),
		data.NewField("currentPrice", nil, make([]*float64, 0, len(result.Products))),
		data.NewField("commission", nil, make([]*float64, 0, len(result.Products))),
		data.NewField("commissionRatePercent", nil, make([]*float64, 0, len(result.Products))),
		data.NewField("logisticsCost", nil, make([]*float64, 0, len(result.Products))),
		data.NewField("storageCost", nil, make([]*float64, 0, len(result.Products))),
		data.NewField("costPrice", nil, make([]*float64, 0, len(result.Products))),
		data.NewField("netMargin", nil, make([]*float64, 0, len(result.Products))),
		data.NewField("netMarginPercent", nil, make([]*float64, 0, len(result.Products))),
		data.NewField("updatedAt", nil, make([]time.Time, 0, len(result.Products))),
	)
	for _, product := range result.Products {
		productFrame.Fields[0].Append(string(product.Marketplace))
		productFrame.Fields[1].Append(product.ProductID)
		productFrame.Fields[2].Append(product.MarketplaceProductID)
		productFrame.Fields[3].Append(product.SellerSKU)
		productFrame.Fields[4].Append(marketplace.NormalizeSellerSKU(product.SellerSKU))
		productFrame.Fields[5].Append(product.Name)
		productFrame.Fields[6].Append(product.VariantID)
		productFrame.Fields[7].Append(product.VariantName)
		productFrame.Fields[8].Append(product.CurrentPrice)
		productFrame.Fields[9].Append(product.Commission)
		productFrame.Fields[10].Append(product.CommissionRatePercent)
		productFrame.Fields[11].Append(product.LogisticsCost)
		productFrame.Fields[12].Append(product.StorageCost)
		productFrame.Fields[13].Append(product.CostPrice)
		productFrame.Fields[14].Append(product.NetMargin)
		productFrame.Fields[15].Append(product.NetMarginPercent)
		productFrame.Fields[16].Append(product.UpdatedAt)
	}
	productFrame.RefID = query.RefID

	mergedFrame := data.NewFrame(
		"merged-products",
		data.NewField("sellerSku", nil, make([]string, 0, len(result.Groups))),
		data.NewField("offers", nil, make([]string, 0, len(result.Groups))),
	)
	for _, group := range result.Groups {
		offers, marshalErr := json.Marshal(group.Offers)
		if marshalErr != nil {
			err = errors.Join(err, fmt.Errorf("encode merged seller SKU %q: %w", group.SellerSKU, marshalErr))
			continue
		}
		mergedFrame.Fields[0].Append(group.SellerSKU)
		mergedFrame.Fields[1].Append(string(offers))
	}
	mergedFrame.RefID = query.RefID

	statusFrame := data.NewFrame(
		"marketplace-status",
		data.NewField("marketplace", nil, make([]string, 0, len(result.Providers))),
		data.NewField("state", nil, make([]string, 0, len(result.Providers))),
		data.NewField("productCount", nil, make([]int64, 0, len(result.Providers))),
		data.NewField("error", nil, make([]string, 0, len(result.Providers))),
	)
	for _, status := range result.Providers {
		statusFrame.Fields[0].Append(string(status.Marketplace))
		statusFrame.Fields[1].Append(string(status.State))
		statusFrame.Fields[2].Append(int64(status.ProductCount))
		statusFrame.Fields[3].Append(status.Error)
	}
	statusFrame.RefID = query.RefID

	warningFrame := data.NewFrame(
		"merge-warnings",
		data.NewField("sellerSku", nil, make([]string, 0, len(result.Warnings))),
		data.NewField("marketplace", nil, make([]string, 0, len(result.Warnings))),
		data.NewField("productIds", nil, make([]string, 0, len(result.Warnings))),
		data.NewField("reason", nil, make([]string, 0, len(result.Warnings))),
	)
	for _, warning := range result.Warnings {
		productIDs, marshalErr := json.Marshal(warning.ProductIDs)
		if marshalErr != nil {
			err = errors.Join(err, fmt.Errorf("encode merge warning product IDs: %w", marshalErr))
			continue
		}
		warningFrame.Fields[0].Append(warning.SellerSKU)
		warningFrame.Fields[1].Append(string(warning.Marketplace))
		warningFrame.Fields[2].Append(string(productIDs))
		warningFrame.Fields[3].Append(warning.Reason)
	}
	warningFrame.RefID = query.RefID

	response := backend.DataResponse{Frames: data.Frames{productFrame, mergedFrame, statusFrame, warningFrame}}
	response.Error = err
	return response
}

func (d *Datasource) query(ctx context.Context, query backend.DataQuery) backend.DataResponse {
	var model queryModel
	if err := json.Unmarshal(query.JSON, &model); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, "invalid query JSON")
	}

	route, ok := marketplaceRoutes[model.Marketplace]
	if !ok {
		return backend.ErrDataResponse(backend.StatusBadRequest, "unknown marketplace route")
	}
	requestPath, err := validateRequestPath(model.Path)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}
	method := strings.ToUpper(model.Method)
	if method != http.MethodGet && method != http.MethodPost {
		return backend.ErrDataResponse(backend.StatusBadRequest, "method must be GET or POST")
	}
	if len(model.Body) > maxRequestBytes {
		return backend.ErrDataResponse(backend.StatusBadRequest, "request body exceeds 1 MiB")
	}
	if err := d.validateCredentials(model.Marketplace); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}

	targetURL := route.host + requestPath
	var body io.Reader
	if method == http.MethodPost && model.Body != "" {
		body = bytes.NewBufferString(model.Body)
	}
	request, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, "could not create marketplace request")
	}
	d.applyCredentials(request, model.Marketplace)
	if method == http.MethodPost && model.Body != "" {
		request.Header.Set("Content-Type", "application/json")
	}

	upstreamResponse, err := d.client.Do(request)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadGateway, fmt.Sprintf("marketplace request failed: %v", err))
	}
	defer upstreamResponse.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(upstreamResponse.Body, maxResponseBytes+1))
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadGateway, "could not read marketplace response")
	}
	if len(responseBody) > maxResponseBytes {
		return backend.ErrDataResponse(backend.StatusBadGateway, "marketplace response exceeds 5 MiB")
	}
	if upstreamResponse.StatusCode < http.StatusOK || upstreamResponse.StatusCode >= http.StatusMultipleChoices {
		return backend.ErrDataResponse(
			backend.StatusBadGateway,
			fmt.Sprintf("marketplace API returned HTTP %d", upstreamResponse.StatusCode),
		)
	}

	frame := data.NewFrame(
		"marketplace-response",
		data.NewField("marketplace", nil, []string{model.Marketplace}),
		data.NewField("status", nil, []int64{int64(upstreamResponse.StatusCode)}),
		data.NewField("response", nil, []string{string(responseBody)}),
	)
	frame.RefID = query.RefID
	return backend.DataResponse{Frames: data.Frames{frame}}
}

func validateRequestPath(value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return "", fmt.Errorf("API path must be an absolute path beginning with one slash")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
		return "", fmt.Errorf("API path must be relative to the selected marketplace host")
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == ".." {
			return "", fmt.Errorf("API path cannot contain parent-directory segments")
		}
	}
	return parsed.String(), nil
}

func (d *Datasource) validateCredentials(route string) error {
	switch route {
	case "wb-tariffs", "wb-prices":
		if d.credentials.wildberriesToken == "" {
			return fmt.Errorf("configure a Wildberries API token")
		}
	case "ozon":
		if d.credentials.ozonClientID == "" || d.credentials.ozonAPIKey == "" {
			return fmt.Errorf("configure both Ozon Client-Id and API key")
		}
	}
	return nil
}

func (d *Datasource) applyCredentials(request *http.Request, route string) {
	switch route {
	case "wb-tariffs", "wb-prices":
		request.Header.Set("Authorization", d.credentials.wildberriesToken)
	case "ozon":
		request.Header.Set("Client-Id", d.credentials.ozonClientID)
		request.Header.Set("Api-Key", d.credentials.ozonAPIKey)
	}
}

func (d *Datasource) CheckHealth(_ context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if d.credentials.wildberriesToken == "" && (d.credentials.ozonClientID == "" || d.credentials.ozonAPIKey == "") {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Configure a Wildberries token or both Ozon credentials",
		}, nil
	}
	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Marketplace credentials are configured",
	}, nil
}
