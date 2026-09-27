package marketplace

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type testProvider struct {
	marketplace Marketplace
	products    []ProductMetrics
	err         error
	calls       int
}

func (p *testProvider) Marketplace() Marketplace { return p.marketplace }

func (p *testProvider) GetProductMetrics(context.Context) ([]ProductMetrics, error) {
	p.calls++
	return p.products, p.err
}

func TestNormalizeSellerSKU(t *testing.T) {
	if got := NormalizeSellerSKU("  wb-AbC-42\n"); got != "WB-ABC-42" {
		t.Fatalf("NormalizeSellerSKU() = %q, want %q", got, "WB-ABC-42")
	}
}

func TestMergeGroupsByExplicitSKUAndReportsAmbiguity(t *testing.T) {
	products := []ProductMetrics{
		{Marketplace: Ozon, ProductID: "oz-2", SellerSKU: " sku-a "},
		{Marketplace: Wildberries, ProductID: "wb-1", SellerSKU: "SKU-A"},
		{Marketplace: Wildberries, ProductID: "wb-2", SellerSKU: "sku-a"},
		{Marketplace: Ozon, ProductID: "oz-no-sku"},
		{Marketplace: Wildberries, ProductID: "wb-no-sku", SellerSKU: "  "},
	}

	groups, warnings := Merge(products)
	if len(groups) != 3 {
		t.Fatalf("Merge() returned %d groups, want 3: %#v", len(groups), groups)
	}
	if groups[0].SellerSKU != "" || len(groups[0].Offers) != 1 || groups[0].Offers[0].ProductID != "oz-no-sku" {
		t.Fatalf("first missing-SKU group = %#v", groups[0])
	}
	if groups[1].SellerSKU != "" || len(groups[1].Offers) != 1 || groups[1].Offers[0].ProductID != "wb-no-sku" {
		t.Fatalf("second missing-SKU group = %#v", groups[1])
	}
	if groups[2].SellerSKU != "SKU-A" || len(groups[2].Offers) != 3 {
		t.Fatalf("matched group = %#v", groups[2])
	}
	if groups[2].Offers[0].Marketplace != Ozon || groups[2].Offers[1].ProductID != "wb-1" || groups[2].Offers[2].ProductID != "wb-2" {
		t.Fatalf("offers are not stable-sorted: %#v", groups[2].Offers)
	}
	if len(warnings) != 3 {
		t.Fatalf("Merge() returned %d warnings, want 3: %#v", len(warnings), warnings)
	}
	wantReasons := []string{
		"missing_seller_sku",
		"missing_seller_sku",
		"duplicate_seller_sku_in_marketplace",
	}
	for index := range wantReasons {
		if warnings[index].Reason != wantReasons[index] {
			t.Errorf("warning %d reason = %q, want %q", index, warnings[index].Reason, wantReasons[index])
		}
	}
	if !reflect.DeepEqual(warnings[2].ProductIDs, []string{"wb-1", "wb-2"}) {
		t.Errorf("duplicate warning product IDs = %#v", warnings[2].ProductIDs)
	}
}

func TestMergeDoesNotOverwriteMarketplaceMetrics(t *testing.T) {
	wbPrice, ozonPrice := 100.0, 120.0
	groups, _ := Merge([]ProductMetrics{
		{Marketplace: Wildberries, ProductID: "101", SellerSKU: "sku", CurrentPrice: &wbPrice},
		{Marketplace: Ozon, ProductID: "202", SellerSKU: "SKU", CurrentPrice: &ozonPrice},
	})
	if len(groups) != 1 || len(groups[0].Offers) != 2 {
		t.Fatalf("Merge() = %#v, want one group with two offers", groups)
	}
	if *groups[0].Offers[0].CurrentPrice != 120 || *groups[0].Offers[1].CurrentPrice != 100 {
		t.Fatalf("marketplace-specific prices were overwritten: %#v", groups[0].Offers)
	}
}

func TestCollectPartialProviderFailures(t *testing.T) {
	product := ProductMetrics{Marketplace: Ozon, ProductID: "o1", SellerSKU: "sku"}
	tests := []struct {
		name      string
		providers []*testProvider
		wantCount int
		wantState []ProviderState
	}{
		{
			name: "Wildberries unavailable",
			providers: []*testProvider{
				{marketplace: Wildberries, err: errors.New("wb offline")},
				{marketplace: Ozon, products: []ProductMetrics{product}},
			},
			wantCount: 1,
			wantState: []ProviderState{ProviderUnavailable, ProviderAvailable},
		},
		{
			name: "Ozon unavailable",
			providers: []*testProvider{
				{marketplace: Wildberries, products: []ProductMetrics{{Marketplace: Wildberries, ProductID: "w1"}}},
				{marketplace: Ozon, err: errors.New("ozon offline")},
			},
			wantCount: 1,
			wantState: []ProviderState{ProviderAvailable, ProviderUnavailable},
		},
		{
			name: "both unavailable",
			providers: []*testProvider{
				{marketplace: Wildberries, err: errors.New("wb offline")},
				{marketplace: Ozon, err: errors.New("ozon offline")},
			},
			wantCount: 0,
			wantState: []ProviderState{ProviderUnavailable, ProviderUnavailable},
		},
		{
			name: "partial data",
			providers: []*testProvider{
				{marketplace: Wildberries, products: []ProductMetrics{{Marketplace: Wildberries, ProductID: "w1"}}, err: errors.New("commission API offline")},
				{marketplace: Ozon},
			},
			wantCount: 1,
			wantState: []ProviderState{ProviderPartial, ProviderNoData},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			providers := make([]Provider, len(test.providers))
			for index, provider := range test.providers {
				providers[index] = provider
			}
			result, err := Collect(context.Background(), providers...)
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if len(result.Products) != test.wantCount {
				t.Fatalf("Collect() returned %d products, want %d", len(result.Products), test.wantCount)
			}
			if len(result.Providers) != len(test.wantState) {
				t.Fatalf("Collect() returned %d statuses, want %d", len(result.Providers), len(test.wantState))
			}
			for index, want := range test.wantState {
				if result.Providers[index].State != want {
					t.Errorf("provider status %d = %q, want %q", index, result.Providers[index].State, want)
				}
			}
		})
	}
}

func TestCollectPreservesDataFromFailedProvider(t *testing.T) {
	product := ProductMetrics{Marketplace: Wildberries, ProductID: "101"}
	provider := &testProvider{marketplace: Wildberries, products: []ProductMetrics{product}, err: errors.New("commission data unavailable")}
	result, err := Collect(context.Background(), provider)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(result.Products) != 1 || result.Products[0].ProductID != product.ProductID {
		t.Fatalf("Collect() lost partial product data: %#v", result.Products)
	}
	if result.Providers[0].State != ProviderPartial || result.Providers[0].Error == "" {
		t.Fatalf("provider status = %#v, want partial state and error", result.Providers[0])
	}
}

func TestCollectCancellationStopsRemainingProviders(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wb := &testProvider{marketplace: Wildberries}
	ozon := &testProvider{marketplace: Ozon}
	result, err := Collect(ctx, wb, ozon)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Collect() error = %v, want context.Canceled", err)
	}
	if wb.calls != 0 || ozon.calls != 0 {
		t.Fatalf("provider calls = (%d, %d), want (0, 0)", wb.calls, ozon.calls)
	}
	if len(result.Providers) != 2 || result.Providers[0].State != ProviderCancelled || result.Providers[1].State != ProviderCancelled {
		t.Fatalf("cancellation statuses = %#v", result.Providers)
	}
}

func TestCollectCancellationDuringProviderPreservesPartialData(t *testing.T) {
	wb := &testProvider{
		marketplace: Wildberries,
		products:    []ProductMetrics{{Marketplace: Wildberries, ProductID: "101"}},
		err:         context.Canceled,
	}
	ozon := &testProvider{marketplace: Ozon}
	result, err := Collect(context.Background(), wb, ozon)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Collect() error = %v, want context.Canceled", err)
	}
	if len(result.Products) != 1 || len(result.Providers) != 2 || result.Providers[0].State != ProviderCancelled || result.Providers[1].State != ProviderCancelled {
		t.Fatalf("result after cancellation = %#v", result)
	}
	if ozon.calls != 0 {
		t.Errorf("Ozon provider was called %d times after cancellation", ozon.calls)
	}
}

func TestProductMetricsLeavesUnavailableFinancialValuesNil(t *testing.T) {
	metrics := ProductMetrics{Marketplace: Ozon, ProductID: "o1"}
	if metrics.CurrentPrice != nil || metrics.Commission != nil || metrics.LogisticsCost != nil ||
		metrics.StorageCost != nil || metrics.NetMargin != nil || metrics.NetMarginPercent != nil {
		t.Fatalf("unknown metrics should remain nil: %+v", metrics)
	}
}
