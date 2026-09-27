package plugin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/pumba3000lvl/marketplace-profit-monitor/pkg/marketplace"
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

func TestMetricsQueryReturnsProductStatusAndWarningFrames(t *testing.T) {
	query := backend.DataQuery{
		RefID: "A",
		JSON:  json.RawMessage(`{"marketplace":"metrics"}`),
	}
	if !isMetricsQuery(query) {
		t.Fatal("isMetricsQuery() = false for metrics mode")
	}
	response := (&Datasource{}).queryMetrics(context.Background(), query)
	if response.Error != nil {
		t.Fatalf("queryMetrics() error = %v", response.Error)
	}
	if len(response.Frames) != 4 {
		t.Fatalf("queryMetrics() returned %d frames, want 4", len(response.Frames))
	}
	if response.Frames[0].Name != "product-metrics" || response.Frames[1].Name != "merged-products" ||
		response.Frames[2].Name != "marketplace-status" || response.Frames[3].Name != "merge-warnings" {
		t.Fatalf("query frame names = %q, %q, %q, %q", response.Frames[0].Name, response.Frames[1].Name, response.Frames[2].Name, response.Frames[3].Name)
	}
	statusFrame := response.Frames[2]
	if statusFrame.Rows() != 2 {
		t.Fatalf("marketplace status rows = %d, want 2", statusFrame.Rows())
	}
	for index := 0; index < statusFrame.Rows(); index++ {
		if statusFrame.Fields[1].At(index) != "unavailable" || statusFrame.Fields[3].At(index) == "" {
			t.Errorf("status row %d = (%v, %v), want unavailable with an error", index, statusFrame.Fields[1].At(index), statusFrame.Fields[3].At(index))
		}
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
