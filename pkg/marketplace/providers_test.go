package marketplace

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/pumba3000lvl/marketplace-profit-monitor/pkg/ozon"
	"github.com/pumba3000lvl/marketplace-profit-monitor/pkg/wildberries"
)

type fakeWildberriesAPI struct {
	products      []wildberries.Product
	commissions   []wildberries.CommissionItem
	productErr    error
	commissionErr error
}

func (f fakeWildberriesAPI) GetProducts(context.Context, int, int) ([]wildberries.Product, error) {
	return f.products, f.productErr
}

func (f fakeWildberriesAPI) GetCommissions(context.Context, string) ([]wildberries.CommissionItem, error) {
	return f.commissions, f.commissionErr
}

type fakeOzonAPI struct {
	products   []ozon.Product
	prices     []ozon.PriceItem
	productErr error
	priceErr   error
}

func (f fakeOzonAPI) GetProductList(context.Context, int, string) ([]ozon.Product, error) {
	return f.products, f.productErr
}

func (f fakeOzonAPI) GetPrices(context.Context, []string) ([]ozon.PriceItem, error) {
	return f.prices, f.priceErr
}

func TestWildberriesProviderMapsAvailableMetrics(t *testing.T) {
	provider := NewWildberriesProvider(fakeWildberriesAPI{
		products: []wildberries.Product{{
			NmID:                123,
			VendorCode:          " sku-1 ",
			SubjectID:           12,
			CurrencyIsoCode4217: "643",
			Sizes: []wildberries.ProductSize{
				{SizeID: 1, TechSizeName: "S", Price: 120, DiscountedPrice: 99.95},
				{SizeID: 2, TechSizeName: "M", Price: 120, DiscountedPrice: 0},
			},
		}},
		commissions: []wildberries.CommissionItem{{SubjectID: 12, KgvpSupplier: 15}},
	})
	metrics, err := provider.GetProductMetrics(context.Background())
	if err != nil {
		t.Fatalf("GetProductMetrics() error = %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("GetProductMetrics() returned %d metrics, want 2 sizes", len(metrics))
	}
	if metrics[0].Marketplace != Wildberries || metrics[0].ProductID != "123" || metrics[0].SellerSKU != " sku-1 " {
		t.Fatalf("Wildberries identifiers = %+v", metrics[0])
	}
	if *metrics[0].CurrentPrice != 99.95 || *metrics[0].CommissionRatePercent != 15 || *metrics[0].Commission != 14.99 {
		t.Errorf("mapped price and commission = %+v, want 99.95 RUB and 15%% / 14.99 RUB", metrics[0])
	}
	if *metrics[1].CurrentPrice != 120 {
		t.Errorf("fallback Wildberries price = %v, want 120", *metrics[1].CurrentPrice)
	}
	if metrics[0].LogisticsCost != nil || metrics[0].StorageCost != nil || metrics[0].NetMargin != nil {
		t.Errorf("unavailable costs or margin were fabricated: %+v", metrics[0])
	}
	if metrics[0].UpdatedAt.IsZero() || !metrics[0].UpdatedAt.Equal(metrics[1].UpdatedAt) {
		t.Errorf("UpdatedAt values are not a shared retrieval timestamp: %v / %v", metrics[0].UpdatedAt, metrics[1].UpdatedAt)
	}
}

func TestWildberriesProviderLeavesUnsupportedValuesUnavailable(t *testing.T) {
	provider := NewWildberriesProvider(fakeWildberriesAPI{
		products: []wildberries.Product{{
			NmID:                123,
			SubjectID:           55,
			CurrencyIsoCode4217: "840",
			Sizes:               []wildberries.ProductSize{{Price: 100}},
		}},
		commissions: []wildberries.CommissionItem{{SubjectID: 99, KgvpSupplier: 20}},
	})
	metrics, err := provider.GetProductMetrics(context.Background())
	if err != nil {
		t.Fatalf("GetProductMetrics() error = %v", err)
	}
	if metrics[0].CurrentPrice != nil || metrics[0].Commission != nil || metrics[0].CommissionRatePercent != nil {
		t.Fatalf("unsupported currency or unmatched commission should be nil: %+v", metrics[0])
	}
}

func TestWildberriesProviderRetainsProductsWhenCommissionsFail(t *testing.T) {
	provider := NewWildberriesProvider(fakeWildberriesAPI{
		products:      []wildberries.Product{{NmID: 123}},
		commissionErr: errors.New("rate limit"),
	})
	metrics, err := provider.GetProductMetrics(context.Background())
	if err == nil || len(metrics) != 1 {
		t.Fatalf("GetProductMetrics() = (%d metrics, %v), want one partial metric and an error", len(metrics), err)
	}
	if metrics[0].Commission != nil {
		t.Fatalf("commission should be unavailable: %+v", metrics[0])
	}
}

func TestWildberriesProviderReportsInvalidCommissionRate(t *testing.T) {
	provider := NewWildberriesProvider(fakeWildberriesAPI{
		products: []wildberries.Product{{
			NmID:                123,
			SubjectID:           12,
			CurrencyIsoCode4217: "643",
			Sizes:               []wildberries.ProductSize{{DiscountedPrice: 100}},
		}},
		commissions: []wildberries.CommissionItem{{SubjectID: 12, KgvpSupplier: 101}},
	})
	metrics, err := provider.GetProductMetrics(context.Background())
	if err == nil || len(metrics) != 1 {
		t.Fatalf("GetProductMetrics() = (%d metrics, %v), want metric and validation error", len(metrics), err)
	}
	if metrics[0].Commission != nil {
		t.Fatalf("invalid commission amount should remain unavailable: %+v", metrics[0])
	}
}

func TestOzonProviderMapsDecimalStringPrices(t *testing.T) {
	provider := NewOzonProvider(fakeOzonAPI{
		products: []ozon.Product{
			{ProductID: "1001", OfferID: "SKU-1"},
			{ProductID: "1002", OfferID: "SKU-2"},
		},
		prices: []ozon.PriceItem{
			{ProductID: "1001", Price: "99.95", CurrencyCode: "RUB"},
			{ProductID: "1002", Price: "0.005", CurrencyCode: "RUB"},
		},
	})
	metrics, err := provider.GetProductMetrics(context.Background())
	if err != nil {
		t.Fatalf("GetProductMetrics() error = %v", err)
	}
	if len(metrics) != 2 || *metrics[0].CurrentPrice != 99.95 || *metrics[1].CurrentPrice != 0.01 {
		t.Fatalf("Ozon decimal price mapping = %#v", metrics)
	}
	if metrics[0].Marketplace != Ozon || metrics[0].ProductID != "SKU-1" ||
		metrics[0].MarketplaceProductID != "1001" || metrics[0].SellerSKU != "SKU-1" {
		t.Fatalf("Ozon identifiers = %+v", metrics[0])
	}
	if metrics[0].Commission != nil || metrics[0].LogisticsCost != nil || metrics[0].StorageCost != nil || metrics[0].NetMargin != nil {
		t.Fatalf("unavailable Ozon financials were fabricated: %+v", metrics[0])
	}
}

func TestOzonProviderSkipsNonRUBPrices(t *testing.T) {
	provider := NewOzonProvider(fakeOzonAPI{
		products: []ozon.Product{{ProductID: "1001", OfferID: "SKU-1"}},
		prices:   []ozon.PriceItem{{ProductID: "1001", Price: "100.00", CurrencyCode: "USD"}},
	})
	metrics, err := provider.GetProductMetrics(context.Background())
	if err != nil {
		t.Fatalf("GetProductMetrics() error = %v", err)
	}
	if metrics[0].CurrentPrice != nil {
		t.Fatalf("non-RUB price = %v, want unavailable", *metrics[0].CurrentPrice)
	}
}

func TestOzonProviderReportsInvalidDecimalPriceWithoutLosingProduct(t *testing.T) {
	provider := NewOzonProvider(fakeOzonAPI{
		products: []ozon.Product{{ProductID: "1001", OfferID: "SKU-1"}},
		prices:   []ozon.PriceItem{{ProductID: "1001", Price: "-12.00", CurrencyCode: "RUB"}},
	})
	metrics, err := provider.GetProductMetrics(context.Background())
	if err == nil || len(metrics) != 1 {
		t.Fatalf("GetProductMetrics() = (%d metrics, %v), want metric and parse error", len(metrics), err)
	}
	if metrics[0].CurrentPrice != nil {
		t.Fatalf("invalid price should be unavailable: %+v", metrics[0])
	}
}

func TestParseRublesUsesIntegerKopecks(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{input: "0.01", want: 1},
		{input: "4.995", want: 500},
		{input: "25.40", want: 2540},
	}
	for _, test := range tests {
		got, err := parseRubles(test.input)
		if err != nil || got != test.want {
			t.Errorf("parseRubles(%q) = (%d, %v), want (%d, nil)", test.input, got, err, test.want)
		}
	}
	for _, input := range []string{"", "-1", "NaN", "92233720368547759"} {
		if _, err := parseRubles(input); err == nil {
			t.Errorf("parseRubles(%q) succeeded, want error", input)
		}
	}
}

func TestCommissionAmountRoundsToNearestKopeck(t *testing.T) {
	amount, err := commissionAmount(9995, 15)
	if err != nil || *amount != 1499 {
		t.Fatalf("commissionAmount() = (%v, %v), want 1499 kopecks", amount, err)
	}
	if _, err := commissionAmount(100, math.MaxFloat64); err == nil {
		t.Fatal("commissionAmount() accepted a non-finite rate")
	}
}

func TestCalculateNetMarginRequiresEveryCostAndUsesKopecks(t *testing.T) {
	metrics := ProductMetrics{}
	if err := CalculateNetMargin(&metrics); err == nil {
		t.Fatal("CalculateNetMargin() succeeded with missing inputs")
	}
	price, commission, logistics, storage, cost := 100.0, 15.0, 5.0, 1.25, 40.0
	metrics = ProductMetrics{
		CurrentPrice:  &price,
		Commission:    &commission,
		LogisticsCost: &logistics,
		StorageCost:   &storage,
		CostPrice:     &cost,
	}
	if err := CalculateNetMargin(&metrics); err != nil {
		t.Fatalf("CalculateNetMargin() error = %v", err)
	}
	if *metrics.NetMargin != 38.75 || math.Abs(*metrics.NetMarginPercent-38.75) > 1e-12 {
		t.Fatalf("margin = %v RUB (%.5f%%), want 38.75 RUB (38.75%%)", *metrics.NetMargin, *metrics.NetMarginPercent)
	}
}
