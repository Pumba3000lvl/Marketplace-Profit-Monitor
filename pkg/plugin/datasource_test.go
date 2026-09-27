package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/pumba3000lvl/marketplace-profit-monitor/pkg/marketplace"
	"github.com/pumba3000lvl/marketplace-profit-monitor/pkg/ozon"
	"github.com/pumba3000lvl/marketplace-profit-monitor/pkg/wildberries"
)

func TestValidateRequestPath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "relative API path", input: "/api/v1/tariffs?limit=10"},
		{name: "absolute URL", input: "https://example.com/api", wantErr: true},
		{name: "network path reference", input: "//example.com/api", wantErr: true},
		{name: "missing leading slash", input: "api/v1/tariffs", wantErr: true},
		{name: "parent segment", input: "/api/../private", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateRequestPath(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateRequestPath(%q) error = %v, wantErr %v", test.input, err, test.wantErr)
			}
		})
	}
}

func TestMarketplaceRoutesUseExpectedAPIHosts(t *testing.T) {
	want := map[string]string{
		"wb-tariffs": "https://common-api.wildberries.ru",
		"wb-prices":  "https://discounts-prices-api.wildberries.ru",
		"ozon":       "https://api-seller.ozon.ru",
	}
	for route, host := range want {
		if got := marketplaceRoutes[route].host; got != host {
			t.Errorf("route %q host = %q, want %q", route, got, host)
		}
	}
}

func TestQueryModelReadsEditorOptions(t *testing.T) {
	var model queryModel
	err := json.Unmarshal([]byte(`{
		"marketplace":"metrics",
		"selectedMarketplace":"both",
		"queryType":"profitability",
		"categories":["wb:electronics","ozon:electronics"],
		"limit":1000,
		"path":"/metrics",
		"method":"GET"
	}`), &model)
	if err != nil {
		t.Fatalf("decode query model: %v", err)
	}
	if model.SelectedMarketplace != "both" || model.QueryType != "profitability" ||
		len(model.Categories) != 2 || model.Categories[0] != "wb:electronics" ||
		model.Categories[1] != "ozon:electronics" || model.Limit == nil || *model.Limit != 1000 {
		t.Fatalf("query model editor options = %+v, want marketplace/type/categories/limit preserved", model)
	}
}

func TestAlertQueryReturnsLabeledSeriesOnlyForKnownValues(t *testing.T) {
	marginPercent, marginRUB := 8.5, -12.25
	datasource := &Datasource{
		credentials: credentials{wildberriesToken: "wb-key"},
		wildberriesProviderFactory: func() (marketplace.Provider, func() error, error) {
			return testProvider{marketplace: marketplace.Wildberries, products: []marketplace.ProductMetrics{
				{
					Marketplace:      marketplace.Wildberries,
					ProductID:        "product-1",
					SellerSKU:        "SKU-1",
					UpdatedAt:        time.Date(2026, time.September, 27, 9, 0, 0, 0, time.UTC),
					NetMarginPercent: &marginPercent,
					NetMargin:        &marginRUB,
				},
				{
					Marketplace: marketplace.Wildberries,
					ProductID:   "product-2",
					UpdatedAt:   time.Date(2026, time.September, 27, 9, 0, 0, 0, time.UTC),
				},
			}}, nil, nil
		},
	}
	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID: "A",
		JSON:  json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"wildberries","queryType":"alert","alertMetric":"netMarginPercent"}`),
	}}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	result := response.Responses["A"]
	if result.Error != nil || result.Status != backend.StatusOK || len(result.Frames) != 1 {
		t.Fatalf("alert response = %+v, want one successful alert series", result)
	}
	frame := result.Frames[0]
	if frame.Rows() != 1 || frame.Fields[0].Name != "time" || frame.Fields[1].Name != "value" ||
		frame.Fields[1].Type() != data.FieldTypeFloat64 || frame.Fields[1].At(0) != marginPercent {
		t.Fatalf("alert frame = %+v, want one margin-percent value and timestamp", frame)
	}
	if frame.Fields[1].Labels["marketplace"] != "wb" ||
		frame.Fields[1].Labels["product_id"] != "product-1" ||
	frame.Fields[1].Labels["seller_sku"] != "SKU-1" {
		t.Fatalf("alert labels = %v, want stable product labels", frame.Fields[1].Labels)
	}

	response, err = datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID: "B",
		JSON:  json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"wildberries","queryType":"alert","alertMetric":"netMarginRUB"}`),
	}}})
	if err != nil {
		t.Fatalf("QueryData() without margin error = %v", err)
	}
	result = response.Responses["B"]
	if result.Error != nil || len(result.Frames) != 1 || result.Frames[0].Rows() != 1 ||
		result.Frames[0].Fields[1].At(0) != marginRUB {
		t.Fatalf("net-margin RUB response = %+v, want one known value", result)
	}
}

func TestAlertQueryDoesNotSubstituteUnsupportedOrMissingMetrics(t *testing.T) {
	datasource := &Datasource{
		credentials: credentials{wildberriesToken: "wb-key"},
		wildberriesProviderFactory: func() (marketplace.Provider, func() error, error) {
			return testProvider{marketplace: marketplace.Wildberries, products: []marketplace.ProductMetrics{{
				Marketplace: marketplace.Wildberries,
				ProductID:   "product-1",
				CurrentPrice: floatPointer(100),
				UpdatedAt: time.Date(2026, time.September, 27, 9, 0, 0, 0, time.UTC),
			}}}, nil, nil
		},
	}
	for _, metric := range []string{"netMarginPercent", "commissionIncreasePercent", "competitorPriceDiffPercent", "storageCostToRevenuePercent"} {
		t.Run(metric, func(t *testing.T) {
			response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
				RefID: "A",
				JSON:  json.RawMessage(fmt.Sprintf(`{"marketplace":"metrics","selectedMarketplace":"wildberries","queryType":"alert","alertMetric":%q}`, metric)),
			}}})
			if err != nil {
				t.Fatalf("QueryData() error = %v", err)
			}
			result := response.Responses["A"]
			if result.Error != nil || result.Status != backend.StatusOK ||
				len(result.Frames) != 1 || result.Frames[0].Rows() != 0 {
				t.Fatalf("alert response = %+v, want successful empty series (NoData)", result)
			}
		})
	}
}

func floatPointer(value float64) *float64 { return &value }

type testProvider struct {
	marketplace marketplace.Marketplace
	products    []marketplace.ProductMetrics
	err         error
}

func (p testProvider) Marketplace() marketplace.Marketplace { return p.marketplace }
func (p testProvider) GetProductMetrics(context.Context) ([]marketplace.ProductMetrics, error) {
	return p.products, p.err
}

type testOzonAnalytics struct {
	rows    []ozon.AnalyticsRow
	err     error
	from    string
	to      string
	metrics []string
}

func (a *testOzonAnalytics) GetAnalytics(_ context.Context, from, to string, metrics []string) ([]ozon.AnalyticsRow, error) {
	a.from, a.to, a.metrics = from, to, append([]string(nil), metrics...)
	return a.rows, a.err
}

func TestQueryDataMapsMalformedAndUnknownQueriesPerRefID(t *testing.T) {
	datasource := &Datasource{
		credentials: credentials{wildberriesToken: "configured"},
		wildberriesProviderFactory: func() (marketplace.Provider, func() error, error) {
			return testProvider{marketplace: marketplace.Wildberries}, nil, nil
		},
	}
	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{RefID: "A", JSON: json.RawMessage(`{invalid`)},
			{RefID: "B", JSON: json.RawMessage(`{"marketplace":"metrics","queryType":"not-a-query"}`)},
			{RefID: "C", JSON: json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"wildberries","queryType":"prices"}`)},
		},
	})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	if response.Responses["A"].Status != backend.StatusBadRequest || response.Responses["A"].Error == nil {
		t.Errorf("malformed response = %+v, want bad request", response.Responses["A"])
	}
	if response.Responses["B"].Status != backend.StatusBadRequest || response.Responses["B"].Error == nil {
		t.Errorf("unknown query type response = %+v, want bad request", response.Responses["B"])
	}
	if response.Responses["C"].Error != nil || response.Responses["C"].Status != backend.StatusOK {
		t.Errorf("valid query after bad inputs = %+v, want success", response.Responses["C"])
	}
}

func TestQueryDataRejectsUnsupportedFiltersAndInvalidLimits(t *testing.T) {
	for name, rawQuery := range map[string]string{
		"category filter":   `{"marketplace":"metrics","categories":["wb:electronics"]}`,
		"zero limit":        `{"marketplace":"metrics","limit":0}`,
		"excessive limit":   `{"marketplace":"metrics","limit":100001}`,
		"unknown selection": `{"marketplace":"metrics","selectedMarketplace":"marketplace-x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			response, err := (&Datasource{}).QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
				RefID: "A",
				JSON:  json.RawMessage(rawQuery),
			}}})
			if err != nil {
				t.Fatalf("QueryData() error = %v", err)
			}
			if got := response.Responses["A"].Status; got != backend.StatusBadRequest {
				t.Fatalf("QueryData() status = %v, want bad request", got)
			}
		})
	}
}

func TestMetricsQueryUsesSelectedProviderAndAppliesLimit(t *testing.T) {
	firstPrice, secondPrice := 12.5, 22.5
	factoryCalls := 0
	datasource := &Datasource{
		credentials: credentials{wildberriesToken: "configured"},
		wildberriesProviderFactory: func() (marketplace.Provider, func() error, error) {
			factoryCalls++
			return testProvider{
				marketplace: marketplace.Wildberries,
				products: []marketplace.ProductMetrics{
					{Marketplace: marketplace.Wildberries, ProductID: "1", CurrentPrice: &firstPrice},
					{Marketplace: marketplace.Wildberries, ProductID: "2", CurrentPrice: &secondPrice},
				},
			}, nil, nil
		},
	}
	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID: "A",
		JSON:  json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"wildberries","queryType":"prices","limit":1}`),
	}}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	result := response.Responses["A"]
	if factoryCalls != 1 || result.Error != nil || result.Frames[0].Name != "marketplace-prices" || result.Frames[0].Rows() != 1 {
		t.Fatalf("query result = calls:%d response:%+v; want one WB price row", factoryCalls, result)
	}
	if result.Frames[0].Fields[1].At(0) != "1" || result.Frames[1].Fields[0].At(0) != "wb" {
		t.Fatalf("query returned wrong product/provider: %v / %v", result.Frames[0].Fields[1].At(0), result.Frames[1].Fields[0].At(0))
	}
}

func TestMetricsQueryBothProvidersPreservesDataAndAddsFailureNotices(t *testing.T) {
	price := 10.0
	datasource := &Datasource{
		credentials: credentials{wildberriesToken: "wb-key", ozonClientID: "123", ozonAPIKey: "ozon-key"},
		wildberriesProviderFactory: func() (marketplace.Provider, func() error, error) {
			return testProvider{marketplace: marketplace.Wildberries, products: []marketplace.ProductMetrics{
				{Marketplace: marketplace.Wildberries, ProductID: "wb-1", CurrentPrice: &price},
			}}, nil, nil
		},
		ozonProviderFactory: func() (marketplace.Provider, func() error, error) {
			return testProvider{marketplace: marketplace.Ozon, err: errors.New("provider unavailable")}, nil, nil
		},
	}
	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID: "A",
		JSON:  json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"both","queryType":"prices"}`),
	}}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	result := response.Responses["A"]
	if result.Status != backend.StatusOK || result.Error != nil || len(result.Frames) != 2 {
		t.Fatalf("partial response = %+v, want successful status with preserved frames", result)
	}
	if result.Frames[0].Rows() != 1 || result.Frames[0].Fields[0].At(0) != "wb" ||
		result.Frames[1].Rows() != 2 || !strings.Contains(result.Frames[1].Fields[1].At(1).(string), "unavailable") {
		t.Fatalf("partial result lost provider data/status: %+v", result.Frames)
	}
	assertWarningOnEveryFrame(t, result.Frames, "Ozon data is unavailable.")
}

func TestMetricsQueryBothProvidersUnavailableReturnsUsefulError(t *testing.T) {
	datasource := &Datasource{
		credentials: credentials{wildberriesToken: "wb-key", ozonClientID: "123", ozonAPIKey: "ozon-key"},
		wildberriesProviderFactory: func() (marketplace.Provider, func() error, error) {
			return testProvider{marketplace: marketplace.Wildberries, err: errors.New("WB network unavailable")}, nil, nil
		},
		ozonProviderFactory: func() (marketplace.Provider, func() error, error) {
			return testProvider{marketplace: marketplace.Ozon, err: errors.New("Ozon network unavailable")}, nil, nil
		},
	}
	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID: "A",
		JSON:  json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"both","queryType":"prices"}`),
	}}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	result := response.Responses["A"]
	if result.Status != backend.StatusInternal || result.Error == nil || len(result.Frames) != 0 {
		t.Fatalf("unavailable response = %+v, want useful internal error without empty data frames", result)
	}
	if !strings.Contains(result.Error.Error(), "No marketplace data is available") ||
		strings.Contains(result.Error.Error(), "network unavailable") {
		t.Fatalf("unavailable error = %q, want clear sanitized message", result.Error)
	}
}

func TestMetricsQueryPreservesOzonPartialRowsAndWarns(t *testing.T) {
	price := 10.0
	datasource := &Datasource{
		credentials: credentials{ozonClientID: "123", ozonAPIKey: "ozon-key"},
		ozonProviderFactory: func() (marketplace.Provider, func() error, error) {
			return testProvider{
				marketplace: marketplace.Ozon,
				products: []marketplace.ProductMetrics{{
					Marketplace:  marketplace.Ozon,
					ProductID:    "ozon-1",
					CurrentPrice: &price,
				}},
				err: errors.New("price API failed after product rows loaded"),
			}, nil, nil
		},
	}
	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID: "O",
		JSON:  json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"ozon","queryType":"prices"}`),
	}}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	result := response.Responses["O"]
	if result.Error != nil || result.Status != backend.StatusOK || len(result.Frames) != 2 {
		t.Fatalf("partial Ozon result = %+v, want successful response with frames", result)
	}
	if result.Frames[0].Rows() != 1 || result.Frames[0].Fields[1].At(0) != "ozon-1" ||
		result.Frames[0].Fields[6].At(0) != &price ||
		result.Frames[1].Fields[1].At(0) != "partial" {
		t.Fatalf("partial Ozon row/status was not retained: %+v", result.Frames)
	}
	assertWarningOnEveryFrame(t, result.Frames, "Ozon returned partial data; some values may be missing.")
}

func TestMetricsQueryExpiredCredentialProvidesSettingsHint(t *testing.T) {
	const secret = "expired-private-token"
	datasource := &Datasource{
		credentials: credentials{wildberriesToken: secret},
		wildberriesProviderFactory: func() (marketplace.Provider, func() error, error) {
			return testProvider{
				marketplace: marketplace.Wildberries,
				err: fmt.Errorf("API rejected token %s: %w", secret, &wildberries.APIError{
					StatusCode: http.StatusUnauthorized,
					Message:    "raw provider response containing a secret",
				}),
			}, nil, nil
		},
	}
	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID: "A",
		JSON:  json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"wildberries","queryType":"prices"}`),
	}}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	result := response.Responses["A"]
	if result.Status != backend.StatusUnauthorized || result.Error == nil ||
		!strings.Contains(result.Error.Error(), "credentials are invalid or expired") ||
		!strings.Contains(result.Error.Error(), "Update them in datasource settings") {
		t.Fatalf("expired credential response = %+v, want unauthorized status and settings hint", result)
	}
	if strings.Contains(result.Error.Error(), secret) || strings.Contains(result.Error.Error(), "raw provider response") {
		t.Fatalf("expired credential response leaked provider detail: %v", result.Error)
	}
}

func TestQueryDataFailureForOneTargetDoesNotAffectLaterTarget(t *testing.T) {
	datasource := &Datasource{
		credentials: credentials{wildberriesToken: "wb-key", ozonClientID: "123", ozonAPIKey: "ozon-key"},
		ozonProviderFactory: func() (marketplace.Provider, func() error, error) {
			return testProvider{marketplace: marketplace.Ozon, err: errors.New("provider unavailable")}, nil, nil
		},
		wildberriesProviderFactory: func() (marketplace.Provider, func() error, error) {
			return testProvider{marketplace: marketplace.Wildberries}, nil, nil
		},
	}
	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{
		{RefID: "A", JSON: json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"ozon","queryType":"prices"}`)},
		{RefID: "B", JSON: json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"wildberries","queryType":"prices"}`)},
	}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	if response.Responses["A"].Error == nil || response.Responses["A"].Status != backend.StatusInternal {
		t.Fatalf("failed target response = %+v, want isolated error", response.Responses["A"])
	}
	if response.Responses["B"].Error != nil || response.Responses["B"].Status != backend.StatusOK ||
		len(response.Responses["B"].Frames) != 2 {
		t.Fatalf("later successful target response = %+v, want success unaffected by A", response.Responses["B"])
	}
}

func TestMetricsQueryMapsRateLimitAndRedactsCredentials(t *testing.T) {
	const secret = "private-wb-token"
	datasource := &Datasource{
		credentials: credentials{wildberriesToken: secret},
		wildberriesProviderFactory: func() (marketplace.Provider, func() error, error) {
			return testProvider{
				marketplace: marketplace.Wildberries,
				err:         fmt.Errorf("request failed with token %s: %w", secret, &wildberries.APIError{StatusCode: http.StatusTooManyRequests}),
			}, nil, nil
		},
	}
	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID: "A",
		JSON:  json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"wildberries","queryType":"profitability"}`),
	}}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	result := response.Responses["A"]
	if result.Status != backend.StatusTooManyRequests || result.Error == nil {
		t.Fatalf("rate-limit response = %+v, want status 429 and error", result)
	}
	if strings.Contains(result.Error.Error(), secret) || strings.Contains(fmt.Sprint(result.Frames), secret) {
		t.Fatalf("response exposed credential %q: %+v", secret, result)
	}
}

func TestOzonCommissionQueryReportsUnavailableCommissionData(t *testing.T) {
	datasource := &Datasource{
		credentials: credentials{ozonClientID: "123", ozonAPIKey: "api-key"},
		ozonProviderFactory: func() (marketplace.Provider, func() error, error) {
			return testProvider{marketplace: marketplace.Ozon}, nil, nil
		},
	}
	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID: "A",
		JSON:  json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"ozon","queryType":"commissions"}`),
	}}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	result := response.Responses["A"]
	if result.Status != backend.StatusOK || result.Error != nil ||
		result.Frames[1].Fields[3].At(0) != "Ozon commission data is not available from the current provider" {
		t.Fatalf("Ozon commission response = %+v, want successful response with explicit warning", result)
	}
	assertWarningOnEveryFrame(t, result.Frames, "Ozon commission data is not available from the current provider")
}

func TestOzonHistoryUsesGrafanaTimeRange(t *testing.T) {
	analytics := &testOzonAnalytics{rows: []ozon.AnalyticsRow{{
		Dimensions: []ozon.AnalyticsDimension{{ID: "2026-09-01", Name: "2026-09-01"}},
		Metrics:    []float64{100.5, 3},
	}}}
	datasource := &Datasource{
		credentials:          credentials{ozonClientID: "123", ozonAPIKey: "api-key"},
		ozonAnalyticsFactory: func() ozonAnalyticsAPI { return analytics },
	}

	from := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.September, 7, 23, 59, 0, 0, time.UTC)
	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID:     "H",
		JSON:      json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"ozon","queryType":"history"}`),
		TimeRange: backend.TimeRange{From: from, To: to},
	}}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	result := response.Responses["H"]
	if result.Error != nil || analytics.from != "2026-09-01" || analytics.to != "2026-09-07" ||
		strings.Join(analytics.metrics, ",") != "revenue,ordered_units" {
		t.Fatalf("history request/result = %q..%q metrics %v response %+v", analytics.from, analytics.to, analytics.metrics, result)
	}
	if result.Frames[0].Rows() != 1 || result.Frames[0].Fields[0].At(0) != "2026-09-01" {
		t.Fatalf("history frame = %+v", result.Frames[0])
	}
}

func TestWildberriesHistoryReportsUnsupportedAPIHistory(t *testing.T) {
	datasource := &Datasource{credentials: credentials{wildberriesToken: "configured"}}
	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID: "H",
		JSON:  json.RawMessage(`{"marketplace":"metrics","selectedMarketplace":"wildberries","queryType":"history"}`),
		TimeRange: backend.TimeRange{
			From: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC),
		},
	}}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	result := response.Responses["H"]
	if result.Status != backend.StatusInternal || result.Error == nil ||
		!strings.Contains(result.Error.Error(), "requires product and upload IDs") {
		t.Fatalf("Wildberries history response = %+v, want explicit unsupported-history error", result)
	}
}

func TestMetricsDataResponsePreservesProductMetricsAndNullableValues(t *testing.T) {
	price, commission := 99.95, 14.99
	updatedAt := time.Date(2026, time.September, 27, 8, 22, 0, 0, time.UTC)
	product := marketplace.ProductMetrics{
		Marketplace:          marketplace.Ozon,
		ProductID:            "offer-1",
		MarketplaceProductID: "123",
		SellerSKU:            " Sku-1 ",
		CurrentPrice:         &price,
		Commission:           &commission,
		UpdatedAt:            updatedAt,
	}

	response := metricsDataResponse(
		backend.DataQuery{RefID: "A"},
		marketplace.CollectionResult{
			Products: []marketplace.ProductMetrics{product},
			Groups:   []marketplace.ProductGroup{{SellerSKU: "SKU-1", Offers: []marketplace.ProductMetrics{product}}},
			Providers: []marketplace.ProviderStatus{
				{Marketplace: marketplace.Wildberries, State: marketplace.ProviderNoData},
				{Marketplace: marketplace.Ozon, State: marketplace.ProviderAvailable, ProductCount: 1},
			},
			Warnings: []marketplace.MergeWarning{{
				Marketplace: marketplace.Wildberries,
				ProductIDs:  []string{"101"},
				Reason:      "missing_seller_sku",
			}},
		},
		nil,
	)
	if response.Error != nil || len(response.Frames) != 4 {
		t.Fatalf("metricsDataResponse() = (%d frames, %v), want 4 frames and no error", len(response.Frames), response.Error)
	}
	productFrame := response.Frames[0]
	if productFrame.Rows() != 1 || productFrame.Fields[1].At(0) != "offer-1" ||
		productFrame.Fields[2].At(0) != "123" || productFrame.Fields[4].At(0) != "SKU-1" {
		t.Fatalf("product row did not preserve IDs or normalized SKU: %#v", productFrame)
	}
	actualPrice, priceOK := productFrame.Fields[8].At(0).(*float64)
	actualCommission, commissionOK := productFrame.Fields[9].At(0).(*float64)
	gotPrice, gotCommission := float64(0), float64(0)
	if priceOK {
		gotPrice = *actualPrice
	}
	if commissionOK {
		gotCommission = *actualCommission
	}
	logisticsValue, logisticsOK := productFrame.Fields[11].At(0).(*float64)
	marginValue, marginOK := productFrame.Fields[14].At(0).(*float64)
	if !priceOK || !commissionOK || gotPrice != price || gotCommission != commission ||
		!logisticsOK || logisticsValue != nil || !marginOK || marginValue != nil {
		t.Fatalf("product financial values = price:%#v (%v/%t) commission:%#v (%v/%t) logistics:%#v margin:%#v want %.2f/%.2f",
			productFrame.Fields[8].At(0), gotPrice, priceOK,
			productFrame.Fields[9].At(0), gotCommission, commissionOK,
			productFrame.Fields[11].At(0), productFrame.Fields[14].At(0), price, commission)
	}
	if productFrame.Fields[16].At(0) != updatedAt {
		t.Errorf("updatedAt = %v, want %v", productFrame.Fields[16].At(0), updatedAt)
	}
	if response.Frames[1].Rows() != 1 || response.Frames[3].Rows() != 1 {
		t.Errorf("merged/warning frame rows = %d/%d, want 1/1", response.Frames[1].Rows(), response.Frames[3].Rows())
	}
}

func assertWarningOnEveryFrame(t *testing.T, frames data.Frames, text string) {
	t.Helper()
	for _, frame := range frames {
		if frame.Meta == nil || len(frame.Meta.Notices) == 0 {
			t.Errorf("frame %q has no metadata notice", frame.Name)
			continue
		}
		found := false
		for _, notice := range frame.Meta.Notices {
			if notice.Severity == data.NoticeSeverityWarning && notice.Text == text {
				found = true
			}
		}
		if !found {
			t.Errorf("frame %q notices = %+v, want warning %q", frame.Name, frame.Meta.Notices, text)
		}
	}
}
