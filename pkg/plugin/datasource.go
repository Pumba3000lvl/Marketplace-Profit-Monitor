package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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
const maxQueryLimit = 100_000

var (
	httpStatusPattern = regexp.MustCompile(`HTTP ([0-9]{3})`)
	requestIDPattern  = regexp.MustCompile(`(?i)request[_ ]id[=: ]+["']?([A-Za-z0-9._-]{1,128})`)
)

type providerFactory func() (marketplace.Provider, func() error, error)

type ozonAnalyticsAPI interface {
	GetAnalytics(context.Context, string, string, []string) ([]ozon.AnalyticsRow, error)
}

type marketplaceRoute struct {
	host string
}

var marketplaceRoutes = map[string]marketplaceRoute{
	"wb-tariffs": {host: "https://common-api.wildberries.ru"},
	"wb-prices":  {host: "https://discounts-prices-api.wildberries.ru"},
	"ozon":       {host: "https://api-seller.ozon.ru"},
}

var (
	_ backend.QueryDataHandler    = (*Datasource)(nil)
	_ backend.CheckHealthHandler  = (*Datasource)(nil)
	_ backend.CallResourceHandler = (*Datasource)(nil)
)

type credentials struct {
	wildberriesToken string
	ozonClientID     string
	ozonAPIKey       string
	telegramBotToken string
}

type datasourceJSONData struct {
	OzonClientID   string `json:"ozonClientId"`
	TelegramChatID string `json:"telegramChatId"`
}

type Datasource struct {
	client                     *http.Client
	credentials                credentials
	jsonData                   datasourceJSONData
	wildberriesProviderFactory providerFactory
	ozonProviderFactory        providerFactory
	ozonAnalyticsFactory       func() ozonAnalyticsAPI
}

type queryModel struct {
	Marketplace         string   `json:"marketplace"`
	SelectedMarketplace string   `json:"selectedMarketplace"`
	QueryType           string   `json:"queryType"`
	Categories          []string `json:"categories"`
	Limit               *int     `json:"limit"`
	Path                string   `json:"path"`
	Method              string   `json:"method"`
	Body                string   `json:"body"`
}

func NewDatasource(_ context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	var jsonData datasourceJSONData
	if len(settings.JSONData) > 0 {
		if err := json.Unmarshal(settings.JSONData, &jsonData); err != nil {
			return nil, fmt.Errorf("decode datasource settings: %w", err)
		}
	}
	return &Datasource{
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		credentials: credentials{
			wildberriesToken: settings.DecryptedSecureJSONData["wildberriesToken"],
			ozonClientID:     jsonData.OzonClientID,
			ozonAPIKey:       settings.DecryptedSecureJSONData["ozonApiKey"],
			telegramBotToken: settings.DecryptedSecureJSONData["telegramBotToken"],
		},
		jsonData: jsonData,
	}, nil
}

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	if req == nil {
		return response, nil
	}
	for _, query := range req.Queries {
		response.Responses[query.RefID] = d.querySafely(ctx, query)
	}
	return response, nil
}

func (d *Datasource) querySafely(ctx context.Context, query backend.DataQuery) (response backend.DataResponse) {
	var model queryModel
	_ = json.Unmarshal(query.JSON, &model)
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("marketplace query panic refID=%q marketplace=%q query_type=%q error=unexpected query failure",
				d.sanitizeString(query.RefID), safeQueryMarketplace(model.Marketplace), safeQueryType(model.QueryType))
			response = backend.ErrDataResponse(backend.StatusInternal, "marketplace query failed unexpectedly")
		}
	}()
	return d.query(ctx, query)
}

func (d *Datasource) query(ctx context.Context, query backend.DataQuery) backend.DataResponse {
	var model queryModel
	if err := json.Unmarshal(query.JSON, &model); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, "invalid query JSON")
	}
	if model.QueryType == "" {
		model.QueryType = "profitability"
	}
	if !isSupportedQueryType(model.QueryType) {
		return backend.ErrDataResponse(backend.StatusBadRequest, "unknown query type")
	}
	if model.Limit != nil && (*model.Limit < 1 || *model.Limit > maxQueryLimit) {
		return backend.ErrDataResponse(backend.StatusBadRequest, "maximum records must be between 1 and 100000")
	}
	if len(model.Categories) > 0 {
		return backend.ErrDataResponse(backend.StatusBadRequest, "category filters are not supported by the marketplace providers")
	}

	if model.Marketplace == "metrics" {
		switch model.QueryType {
		case "history":
			return d.queryHistory(ctx, query, model)
		default:
			return d.queryMetrics(ctx, query, model)
		}
	}
	return d.queryRaw(ctx, query, model)
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

	statusFrame := providerStatusFrame(query.RefID, result.Providers)

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

	response := backend.DataResponse{Frames: data.Frames{productFrame, mergedFrame, statusFrame, warningFrame}, Status: backend.StatusOK}
	response.Error = err
	return response
}

func (d *Datasource) queryRaw(ctx context.Context, query backend.DataQuery, model queryModel) backend.DataResponse {
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
		return backend.ErrDataResponse(backend.StatusInternal, "marketplace request failed")
	}
	defer upstreamResponse.Body.Close()
	if upstreamResponse.StatusCode == http.StatusUnauthorized {
		return backend.ErrDataResponse(backend.StatusUnauthorized, "Marketplace credentials are invalid or expired. Update them in datasource settings.")
	}
	if upstreamResponse.StatusCode == http.StatusTooManyRequests {
		return backend.ErrDataResponse(backend.StatusTooManyRequests, "marketplace API rate limit exceeded")
	}

	responseBody, err := io.ReadAll(io.LimitReader(upstreamResponse.Body, maxResponseBytes+1))
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "could not read marketplace response")
	}
	if len(responseBody) > maxResponseBytes {
		return backend.ErrDataResponse(backend.StatusInternal, "marketplace response exceeds 5 MiB")
	}
	if upstreamResponse.StatusCode < http.StatusOK || upstreamResponse.StatusCode >= http.StatusMultipleChoices {
		return backend.ErrDataResponse(
			backend.StatusInternal,
			fmt.Sprintf("marketplace API returned HTTP %d", upstreamResponse.StatusCode),
		)
	}

	frame := data.NewFrame(
		"marketplace-response",
		data.NewField("marketplace", nil, []string{model.Marketplace}),
		data.NewField("status", nil, []int64{int64(upstreamResponse.StatusCode)}),
		data.NewField("response", nil, []string{d.sanitizeString(string(responseBody))}),
	)
	frame.RefID = query.RefID
	return backend.DataResponse{Frames: data.Frames{frame}, Status: backend.StatusOK}
}

func (d *Datasource) queryMetrics(ctx context.Context, query backend.DataQuery, model queryModel) backend.DataResponse {
	selected, err := selectedMarketplaces(model.SelectedMarketplace)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}

	result := marketplace.CollectionResult{
		Products:  []marketplace.ProductMetrics{},
		Groups:    []marketplace.ProductGroup{},
		Warnings:  []marketplace.MergeWarning{},
		Providers: []marketplace.ProviderStatus{},
	}
	providers := make([]marketplace.Provider, 0, len(selected))
	releases := make([]func() error, 0, len(selected))
	var setupErrors []error
	var setupStatuses []marketplace.ProviderStatus
	missingConfiguration := false

	for _, name := range selected {
		credentialRoute := string(name)
		if name == marketplace.Wildberries {
			credentialRoute = "wildberries"
		}
		if err := d.validateCredentials(credentialRoute); err != nil {
			missingConfiguration = true
			setupErrors = append(setupErrors, fmt.Errorf("%s: %w", name, err))
			setupStatuses = append(setupStatuses, marketplace.ProviderStatus{
				Marketplace: name,
				State:       marketplace.ProviderUnavailable,
				Error:       err.Error(),
			})
			continue
		}

		provider, release, err := d.newProvider(name)
		if err != nil {
			setupErrors = append(setupErrors, fmt.Errorf("%s: initialize provider: %w", name, err))
			setupStatuses = append(setupStatuses, marketplace.ProviderStatus{
				Marketplace: name,
				State:       marketplace.ProviderUnavailable,
				Error:       "could not initialize marketplace provider",
			})
			continue
		}
		providers = append(providers, provider)
		if release != nil {
			releases = append(releases, release)
		}
	}

	var collectErr error
	if len(providers) > 0 {
		result, collectErr = marketplace.CollectWithErrors(ctx, providers...)
	}
	result.Providers = append(result.Providers, setupStatuses...)
	statusesByMarketplace := make(map[marketplace.Marketplace]marketplace.ProviderStatus, len(result.Providers))
	for _, status := range result.Providers {
		statusesByMarketplace[status.Marketplace] = status
	}
	result.Providers = result.Providers[:0]
	for _, name := range selected {
		if status, exists := statusesByMarketplace[name]; exists {
			result.Providers = append(result.Providers, status)
		}
	}

	var releaseErrors []error
	for _, release := range releases {
		if err := release(); err != nil {
			releaseErrors = append(releaseErrors, fmt.Errorf("close marketplace provider: %w", err))
		}
	}
	err = errors.Join(append(setupErrors, collectErr, errors.Join(releaseErrors...))...)
	if len(providers) == 0 && err != nil {
		status := backend.StatusInternal
		if missingConfiguration {
			status = backend.StatusBadRequest
		}
		return backend.ErrDataResponse(status, failureResponseMessage(result.Providers))
	}

	failureMessage := failureResponseMessage(result.Providers)
	d.logProviderFailures(query, model.QueryType, result.Providers)
	for i := range result.Providers {
		result.Providers[i].Error = safeProviderMessage(result.Providers[i].Marketplace, result.Providers[i].State, result.Providers[i].Error)
	}
	if model.QueryType == "commissions" {
		for index := range result.Providers {
			status := &result.Providers[index]
			if status.Marketplace == marketplace.Ozon && status.Error == "" {
				status.State = marketplace.ProviderPartial
				status.Error = "Ozon commission data is not available from the current provider"
			}
		}
	}
	if !providerDataAvailable(result.Providers) {
		status := marketplaceErrorStatus(err)
		if missingConfiguration && collectErr == nil && len(releaseErrors) == 0 {
			status = backend.StatusBadRequest
		}
		return backend.ErrDataResponse(status, failureMessage)
	}

	result.Products = d.sanitizeProducts(limitedProducts(result.Products, model.Limit))
	result.Groups, result.Warnings = marketplace.Merge(result.Products)
	response := metricsQueryDataResponse(query, model.QueryType, result)
	attachProviderNotices(&response, result.Providers)
	return response
}

func (d *Datasource) newProvider(name marketplace.Marketplace) (marketplace.Provider, func() error, error) {
	switch name {
	case marketplace.Wildberries:
		if d.wildberriesProviderFactory != nil {
			return d.wildberriesProviderFactory()
		}
		client := wildberries.NewClient(d.credentials.wildberriesToken)
		return marketplace.NewWildberriesProvider(client), client.Close, nil
	case marketplace.Ozon:
		if d.ozonProviderFactory != nil {
			return d.ozonProviderFactory()
		}
		return marketplace.NewOzonProvider(ozon.NewClient(d.credentials.ozonClientID, d.credentials.ozonAPIKey)), nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported marketplace %q", name)
	}
}

func selectedMarketplaces(value string) ([]marketplace.Marketplace, error) {
	switch value {
	case "", "both":
		return []marketplace.Marketplace{marketplace.Wildberries, marketplace.Ozon}, nil
	case "wildberries":
		return []marketplace.Marketplace{marketplace.Wildberries}, nil
	case "ozon":
		return []marketplace.Marketplace{marketplace.Ozon}, nil
	default:
		return nil, errors.New("unknown marketplace selection")
	}
}

func isSupportedQueryType(value string) bool {
	switch value {
	case "commissions", "prices", "profitability", "history":
		return true
	default:
		return false
	}
}

func limitedProducts(products []marketplace.ProductMetrics, limit *int) []marketplace.ProductMetrics {
	if limit != nil && len(products) > *limit {
		products = products[:*limit]
	}
	return products
}

func metricsQueryDataResponse(query backend.DataQuery, queryType string, result marketplace.CollectionResult) backend.DataResponse {
	if queryType == "profitability" {
		return metricsDataResponse(query, result, nil)
	}

	frame := data.NewFrame("marketplace-"+queryType, data.NewField("marketplace", nil, make([]string, 0, len(result.Products))))
	switch queryType {
	case "prices":
		frame.Fields = append(frame.Fields,
			data.NewField("productId", nil, make([]string, 0, len(result.Products))),
			data.NewField("marketplaceProductId", nil, make([]string, 0, len(result.Products))),
			data.NewField("sellerSku", nil, make([]string, 0, len(result.Products))),
			data.NewField("variantId", nil, make([]string, 0, len(result.Products))),
			data.NewField("variantName", nil, make([]string, 0, len(result.Products))),
			data.NewField("currentPrice", nil, make([]*float64, 0, len(result.Products))),
			data.NewField("updatedAt", nil, make([]time.Time, 0, len(result.Products))),
		)
		for _, product := range result.Products {
			frame.Fields[0].Append(string(product.Marketplace))
			frame.Fields[1].Append(product.ProductID)
			frame.Fields[2].Append(product.MarketplaceProductID)
			frame.Fields[3].Append(product.SellerSKU)
			frame.Fields[4].Append(product.VariantID)
			frame.Fields[5].Append(product.VariantName)
			frame.Fields[6].Append(product.CurrentPrice)
			frame.Fields[7].Append(product.UpdatedAt)
		}
	case "commissions":
		frame.Fields = append(frame.Fields,
			data.NewField("productId", nil, make([]string, 0, len(result.Products))),
			data.NewField("sellerSku", nil, make([]string, 0, len(result.Products))),
			data.NewField("commission", nil, make([]*float64, 0, len(result.Products))),
			data.NewField("commissionRatePercent", nil, make([]*float64, 0, len(result.Products))),
		)
		for _, product := range result.Products {
			frame.Fields[0].Append(string(product.Marketplace))
			frame.Fields[1].Append(product.ProductID)
			frame.Fields[2].Append(product.SellerSKU)
			frame.Fields[3].Append(product.Commission)
			frame.Fields[4].Append(product.CommissionRatePercent)
		}
	}
	frame.RefID = query.RefID
	status := providerStatusFrame(query.RefID, result.Providers)
	return backend.DataResponse{Frames: data.Frames{frame, status}, Status: backend.StatusOK}
}

func providerStatusFrame(refID string, statuses []marketplace.ProviderStatus) *data.Frame {
	frame := data.NewFrame(
		"marketplace-status",
		data.NewField("marketplace", nil, make([]string, 0, len(statuses))),
		data.NewField("state", nil, make([]string, 0, len(statuses))),
		data.NewField("productCount", nil, make([]int64, 0, len(statuses))),
		data.NewField("error", nil, make([]string, 0, len(statuses))),
	)
	for _, status := range statuses {
		frame.Fields[0].Append(string(status.Marketplace))
		frame.Fields[1].Append(string(status.State))
		frame.Fields[2].Append(int64(status.ProductCount))
		frame.Fields[3].Append(status.Error)
	}
	frame.RefID = refID
	return frame
}

func (d *Datasource) queryHistory(ctx context.Context, query backend.DataQuery, model queryModel) backend.DataResponse {
	selected, err := selectedMarketplaces(model.SelectedMarketplace)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}

	var statuses []marketplace.ProviderStatus
	var rows []ozon.AnalyticsRow
	var queryErrors []error
	missingConfiguration := false
	for _, name := range selected {
		if name == marketplace.Wildberries {
			err := fmt.Errorf("%w: price history requires product and upload IDs, which are not available in this query", wildberries.ErrUploadTaskEnumerationUnsupported)
			queryErrors = append(queryErrors, err)
			statuses = append(statuses, marketplace.ProviderStatus{
				Marketplace: name,
				State:       marketplace.ProviderUnavailable,
				Error:       err.Error(),
			})
			continue
		}
		if err := d.validateCredentials("ozon"); err != nil {
			missingConfiguration = true
			queryErrors = append(queryErrors, err)
			statuses = append(statuses, marketplace.ProviderStatus{
				Marketplace: name,
				State:       marketplace.ProviderUnavailable,
				Error:       err.Error(),
			})
			continue
		}
		if query.TimeRange.From.IsZero() || query.TimeRange.To.IsZero() || query.TimeRange.To.Before(query.TimeRange.From) {
			return backend.ErrDataResponse(backend.StatusBadRequest, "history queries require a valid Grafana time range")
		}

		var client ozonAnalyticsAPI
		if d.ozonAnalyticsFactory != nil {
			client = d.ozonAnalyticsFactory()
		} else {
			client = ozon.NewClient(d.credentials.ozonClientID, d.credentials.ozonAPIKey)
		}
		analytics, analyticsErr := client.GetAnalytics(
			ctx,
			query.TimeRange.From.Format("2006-01-02"),
			query.TimeRange.To.Format("2006-01-02"),
			[]string{"revenue", "ordered_units"},
		)
		rows = append(rows, analytics...)
		status := marketplace.ProviderStatus{
			Marketplace:  marketplace.Ozon,
			ProductCount: len(analytics),
			State:        marketplace.ProviderAvailable,
		}
		if analyticsErr != nil {
			queryErrors = append(queryErrors, analyticsErr)
			status.Error = d.sanitizeError(analyticsErr).Error()
			if len(analytics) > 0 {
				status.State = marketplace.ProviderPartial
			} else {
				status.State = marketplace.ProviderUnavailable
			}
		} else if len(analytics) == 0 {
			status.State = marketplace.ProviderNoData
		}
		statuses = append(statuses, status)
	}

	for rowIndex := range rows {
		for dimensionIndex := range rows[rowIndex].Dimensions {
			rows[rowIndex].Dimensions[dimensionIndex].ID = d.sanitizeString(rows[rowIndex].Dimensions[dimensionIndex].ID)
			rows[rowIndex].Dimensions[dimensionIndex].Name = d.sanitizeString(rows[rowIndex].Dimensions[dimensionIndex].Name)
		}
	}
	failureMessage := failureResponseMessage(statuses)
	d.logProviderFailures(query, model.QueryType, statuses)
	for index := range statuses {
		statuses[index].Error = safeProviderMessage(statuses[index].Marketplace, statuses[index].State, statuses[index].Error)
	}
	if !providerDataAvailable(statuses) {
		status := marketplaceErrorStatus(errors.Join(queryErrors...))
		if missingConfiguration && len(queryErrors) == 1 {
			status = backend.StatusBadRequest
		}
		return backend.ErrDataResponse(status, failureMessage)
	}
	response := historyDataResponse(query, limitedAnalytics(rows, model.Limit), statuses)
	attachProviderNotices(&response, statuses)
	return response
}

func limitedAnalytics(rows []ozon.AnalyticsRow, limit *int) []ozon.AnalyticsRow {
	if limit != nil && len(rows) > *limit {
		rows = rows[:*limit]
	}
	return rows
}

func historyDataResponse(query backend.DataQuery, rows []ozon.AnalyticsRow, statuses []marketplace.ProviderStatus) backend.DataResponse {
	frame := data.NewFrame(
		"marketplace-history",
		data.NewField("day", nil, make([]string, 0, len(rows))),
		data.NewField("revenue", nil, make([]*float64, 0, len(rows))),
		data.NewField("orderedUnits", nil, make([]*float64, 0, len(rows))),
	)
	for _, row := range rows {
		day := ""
		if len(row.Dimensions) > 0 {
			day = row.Dimensions[0].ID
			if day == "" {
				day = row.Dimensions[0].Name
			}
		}
		var revenue, orderedUnits *float64
		if len(row.Metrics) > 0 {
			value := row.Metrics[0]
			revenue = &value
		}
		if len(row.Metrics) > 1 {
			value := row.Metrics[1]
			orderedUnits = &value
		}
		frame.Fields[0].Append(day)
		frame.Fields[1].Append(revenue)
		frame.Fields[2].Append(orderedUnits)
	}
	frame.RefID = query.RefID
	return backend.DataResponse{Frames: data.Frames{frame, providerStatusFrame(query.RefID, statuses)}, Status: backend.StatusOK}
}

func marketplaceErrorStatus(err error) backend.Status {
	var wildberriesErr *wildberries.APIError
	if errors.As(err, &wildberriesErr) && wildberriesErr.StatusCode == http.StatusUnauthorized {
		return backend.StatusUnauthorized
	}
	if errors.As(err, &wildberriesErr) && wildberriesErr.StatusCode == http.StatusTooManyRequests {
		return backend.StatusTooManyRequests
	}
	var ozonErr *ozon.APIError
	if errors.As(err, &ozonErr) && ozonErr.StatusCode == http.StatusUnauthorized {
		return backend.StatusUnauthorized
	}
	if errors.As(err, &ozonErr) && ozonErr.StatusCode == http.StatusTooManyRequests {
		return backend.StatusTooManyRequests
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return backend.StatusTimeout
	}
	return backend.StatusInternal
}

func providerDataAvailable(statuses []marketplace.ProviderStatus) bool {
	for _, status := range statuses {
		switch status.State {
		case marketplace.ProviderAvailable, marketplace.ProviderNoData:
			return true
		case marketplace.ProviderPartial:
			if status.ProductCount > 0 ||
				status.Error == "Ozon commission data is not available from the current provider" {
				return true
			}
		}
	}
	return false
}

func attachProviderNotices(response *backend.DataResponse, statuses []marketplace.ProviderStatus) {
	var notices []data.Notice
	for _, status := range statuses {
		if status.State != marketplace.ProviderPartial &&
			status.State != marketplace.ProviderUnavailable &&
			status.State != marketplace.ProviderCancelled {
			continue
		}
		text := safeProviderMessage(status.Marketplace, status.State, status.Error)
		if text == "" {
			continue
		}
		notices = append(notices, data.Notice{Severity: data.NoticeSeverityWarning, Text: text})
	}
	if len(notices) == 0 {
		return
	}
	for _, frame := range response.Frames {
		if frame.Meta == nil {
			frame.Meta = &data.FrameMeta{}
		}
		frame.Meta.Notices = append(frame.Meta.Notices, notices...)
	}
}

func safeProviderMessage(name marketplace.Marketplace, state marketplace.ProviderState, raw string) string {
	label := "Marketplace"
	if name == marketplace.Wildberries {
		label = "Wildberries"
	} else if name == marketplace.Ozon {
		label = "Ozon"
	}
	if status := parsedHTTPStatus(raw); status == http.StatusUnauthorized {
		return label + " credentials are invalid or expired. Update them in datasource settings."
	}
	if status := parsedHTTPStatus(raw); status == http.StatusTooManyRequests {
		return label + " API rate limit exceeded."
	}
	if raw == "Ozon commission data is not available from the current provider" {
		return raw
	}
	if strings.Contains(raw, "price history requires product and upload IDs") {
		return "Wildberries price history requires product and upload IDs, which are not available in this query."
	}
	if strings.Contains(strings.ToLower(raw), "configure ") ||
		strings.Contains(strings.ToLower(raw), "not configured") {
		return label + " is not configured. Update datasource settings."
	}
	switch state {
	case marketplace.ProviderPartial:
		return label + " returned partial data; some values may be missing."
	case marketplace.ProviderUnavailable:
		return label + " data is unavailable."
	case marketplace.ProviderCancelled:
		return label + " request was cancelled."
	default:
		return ""
	}
}

func failureResponseMessage(statuses []marketplace.ProviderStatus) string {
	messages := make([]string, 0, len(statuses))
	for _, status := range statuses {
		message := safeProviderMessage(status.Marketplace, status.State, status.Error)
		if message != "" {
			messages = append(messages, message)
		}
	}
	if len(messages) == 0 {
		return "No marketplace data is available."
	}
	return "No marketplace data is available. " + strings.Join(messages, " ")
}

func parsedHTTPStatus(raw string) int {
	match := httpStatusPattern.FindStringSubmatch(raw)
	if len(match) != 2 {
		return 0
	}
	status, _ := strconv.Atoi(match[1])
	return status
}

func (d *Datasource) logProviderFailures(query backend.DataQuery, queryType string, statuses []marketplace.ProviderStatus) {
	for _, status := range statuses {
		if status.State != marketplace.ProviderPartial &&
			status.State != marketplace.ProviderUnavailable &&
			status.State != marketplace.ProviderCancelled {
			continue
		}
		httpStatus := parsedHTTPStatus(status.Error)
		requestID := ""
		if match := requestIDPattern.FindStringSubmatch(status.Error); len(match) == 2 {
			requestID = strings.Map(func(char rune) rune {
				if char < 32 || char == 127 {
					return -1
				}
				return char
			}, match[1])
		}
		requestID = d.sanitizeString(requestID)
		category := "provider request failed"
		switch httpStatus {
		case http.StatusUnauthorized:
			category = "credentials rejected"
		case http.StatusTooManyRequests:
			category = "rate limited"
		default:
			if httpStatus != 0 {
				category = "upstream API error"
			}
		}
		log.Printf("marketplace query failed refID=%q marketplace=%q query_type=%q status=%d request_id=%q error=%q",
			d.sanitizeString(query.RefID), status.Marketplace, safeQueryType(queryType), httpStatus, requestID, category)
	}
}

func safeQueryMarketplace(value string) string {
	switch value {
	case "metrics", "wildberries", "wb-tariffs", "wb-prices", "ozon":
		return value
	default:
		return "unknown"
	}
}

func safeQueryType(value string) string {
	if isSupportedQueryType(value) {
		return value
	}
	return "unknown"
}

func (d *Datasource) sanitizeError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(d.sanitizeString(err.Error()))
}

func (d *Datasource) sanitizeString(value string) string {
	for _, secret := range []string{
		d.credentials.wildberriesToken,
		d.credentials.ozonClientID,
		d.credentials.ozonAPIKey,
		d.credentials.telegramBotToken,
	} {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	return value
}

func (d *Datasource) sanitizeProducts(products []marketplace.ProductMetrics) []marketplace.ProductMetrics {
	for index := range products {
		products[index].ProductID = d.sanitizeString(products[index].ProductID)
		products[index].MarketplaceProductID = d.sanitizeString(products[index].MarketplaceProductID)
		products[index].SellerSKU = d.sanitizeString(products[index].SellerSKU)
		products[index].Name = d.sanitizeString(products[index].Name)
		products[index].VariantID = d.sanitizeString(products[index].VariantID)
		products[index].VariantName = d.sanitizeString(products[index].VariantName)
	}
	return products
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

func (d *Datasource) applyCredentials(request *http.Request, route string) {
	switch route {
	case "wb-tariffs", "wb-prices":
		request.Header.Set("Authorization", d.credentials.wildberriesToken)
	case "ozon":
		request.Header.Set("Client-Id", d.credentials.ozonClientID)
		request.Header.Set("Api-Key", d.credentials.ozonAPIKey)
	}
}

func (d *Datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	marketplaces, err := d.configuredMarketplaces()
	if err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: err.Error(),
		}, nil
	}
	results := d.testConnections(ctx, marketplaces)
	for _, result := range results {
		if !result.OK {
			return &backend.CheckHealthResult{
				Status:  backend.HealthStatusError,
				Message: result.Marketplace + ": " + result.Message,
			}, nil
		}
	}
	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Marketplace connection successful",
	}, nil
}
