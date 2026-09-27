// Package marketplace normalizes product metrics from supported seller APIs.
package marketplace

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Marketplace string

const (
	Wildberries Marketplace = "wb"
	Ozon        Marketplace = "ozon"
)

// ProductMetrics describes one marketplace offer. Monetary values are RUB;
// nil means the source did not provide enough information to determine them.
type ProductMetrics struct {
	Marketplace           Marketplace `json:"marketplace"`
	ProductID             string      `json:"productId"`
	MarketplaceProductID  string      `json:"marketplaceProductId,omitempty"`
	SellerSKU             string      `json:"sellerSku"`
	Name                  string      `json:"name"`
	VariantID             string      `json:"variantId,omitempty"`
	VariantName           string      `json:"variantName,omitempty"`
	CurrentPrice          *float64    `json:"currentPrice,omitempty"`
	Commission            *float64    `json:"commission,omitempty"`
	CommissionRatePercent *float64    `json:"commissionRatePercent,omitempty"`
	LogisticsCost         *float64    `json:"logisticsCost,omitempty"`
	StorageCost           *float64    `json:"storageCost,omitempty"`
	CostPrice             *float64    `json:"costPrice,omitempty"`
	NetMargin             *float64    `json:"netMargin,omitempty"`
	NetMarginPercent      *float64    `json:"netMarginPercent,omitempty"`
	UpdatedAt             time.Time   `json:"updatedAt"`
}

// Provider retrieves normalized metrics from a single marketplace.
type Provider interface {
	Marketplace() Marketplace
	GetProductMetrics(context.Context) ([]ProductMetrics, error)
}

type ProviderState string

const (
	ProviderAvailable   ProviderState = "available"
	ProviderPartial     ProviderState = "partial"
	ProviderNoData      ProviderState = "no_data"
	ProviderUnavailable ProviderState = "unavailable"
	ProviderCancelled   ProviderState = "cancelled"
)

type ProviderStatus struct {
	Marketplace  Marketplace   `json:"marketplace"`
	State        ProviderState `json:"state"`
	ProductCount int           `json:"productCount"`
	Error        string        `json:"error,omitempty"`
}

type ProductGroup struct {
	// SellerSKU is the normalized join key. Each offer retains its original SKU.
	SellerSKU string           `json:"sellerSku"`
	Offers    []ProductMetrics `json:"offers"`
}

type MergeWarning struct {
	SellerSKU   string      `json:"sellerSku,omitempty"`
	Marketplace Marketplace `json:"marketplace"`
	ProductIDs  []string    `json:"productIds"`
	Reason      string      `json:"reason"`
}

type CollectionResult struct {
	Products  []ProductMetrics `json:"products"`
	Groups    []ProductGroup   `json:"groups"`
	Warnings  []MergeWarning   `json:"mergeWarnings,omitempty"`
	Providers []ProviderStatus `json:"providers"`
}

// Collect retrieves all providers in order, preserving successful and partial
// data if another provider fails. Context cancellation stops further requests.
func Collect(ctx context.Context, providers ...Provider) (CollectionResult, error) {
	result, collectionErr, _ := collect(ctx, providers...)
	return result, collectionErr
}

// CollectWithErrors is Collect plus provider-level failures. It retains
// partial results and returns joined provider errors for callers that need to
// set a response status while still displaying the successful data.
func CollectWithErrors(ctx context.Context, providers ...Provider) (CollectionResult, error) {
	result, collectionErr, providerErr := collect(ctx, providers...)
	return result, errors.Join(collectionErr, providerErr)
}

func collect(ctx context.Context, providers ...Provider) (CollectionResult, error, error) {
	result := CollectionResult{
		Products:  []ProductMetrics{},
		Groups:    []ProductGroup{},
		Warnings:  []MergeWarning{},
		Providers: []ProviderStatus{},
	}
	seen := make(map[Marketplace]struct{}, len(providers))
	for _, provider := range providers {
		if provider == nil {
			return result, errors.New("marketplace provider cannot be nil"), nil
		}
		name := provider.Marketplace()
		if name != Wildberries && name != Ozon {
			return result, fmt.Errorf("unsupported marketplace provider %q", name), nil
		}
		if _, exists := seen[name]; exists {
			return result, fmt.Errorf("duplicate marketplace provider %q", name), nil
		}
		seen[name] = struct{}{}
	}

	var providerErrors []error
	for index, provider := range providers {
		if err := ctx.Err(); err != nil {
			result.Providers = append(result.Providers, ProviderStatus{
				Marketplace: provider.Marketplace(),
				State:       ProviderCancelled,
				Error:       err.Error(),
			})
			for _, remaining := range providers[index+1:] {
				result.Providers = append(result.Providers, ProviderStatus{
					Marketplace: remaining.Marketplace(),
					State:       ProviderCancelled,
					Error:       err.Error(),
				})
			}
			result.Groups, result.Warnings = Merge(result.Products)
			return result, err, errors.Join(providerErrors...)
		}

		products, err := provider.GetProductMetrics(ctx)
		status := ProviderStatus{
			Marketplace:  provider.Marketplace(),
			ProductCount: len(products),
			State:        ProviderAvailable,
		}
		if err != nil {
			providerErrors = append(providerErrors, fmt.Errorf("%s: %w", provider.Marketplace(), err))
			status.Error = err.Error()
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				status.State = ProviderCancelled
			} else if len(products) > 0 {
				status.State = ProviderPartial
			} else {
				status.State = ProviderUnavailable
			}
		} else if len(products) == 0 {
			status.State = ProviderNoData
		}
		result.Providers = append(result.Providers, status)
		result.Products = append(result.Products, products...)
		if status.State == ProviderCancelled {
			cause := err
			if cause == nil {
				cause = ctx.Err()
			}
			for _, remaining := range providers[index+1:] {
				result.Providers = append(result.Providers, ProviderStatus{
					Marketplace: remaining.Marketplace(),
					State:       ProviderCancelled,
					Error:       cause.Error(),
				})
			}
			result.Groups, result.Warnings = Merge(result.Products)
			return result, cause, errors.Join(providerErrors...)
		}
	}

	result.Groups, result.Warnings = Merge(result.Products)
	return result, nil, errors.Join(providerErrors...)
}

// NormalizeSellerSKU trims surrounding whitespace and folds case for matching.
func NormalizeSellerSKU(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

// Merge groups offers only by an explicit seller SKU. Offers without a SKU stay
// separate; duplicate SKUs within one marketplace are retained and reported.
func Merge(products []ProductMetrics) ([]ProductGroup, []MergeWarning) {
	type skuMarketplace struct {
		sku         string
		marketplace Marketplace
	}

	groupsBySKU := make(map[string]*ProductGroup)
	missingSKUOffers := make([]ProductMetrics, 0)
	counts := make(map[skuMarketplace]map[string]struct{})
	for _, product := range products {
		sku := NormalizeSellerSKU(product.SellerSKU)
		if sku == "" {
			missingSKUOffers = append(missingSKUOffers, product)
			continue
		}
		group := groupsBySKU[sku]
		if group == nil {
			group = &ProductGroup{SellerSKU: sku, Offers: []ProductMetrics{}}
			groupsBySKU[sku] = group
		}
		group.Offers = append(group.Offers, product)
		key := skuMarketplace{sku: sku, marketplace: product.Marketplace}
		if counts[key] == nil {
			counts[key] = make(map[string]struct{})
		}
		counts[key][product.ProductID] = struct{}{}
	}

	groups := make([]ProductGroup, 0, len(groupsBySKU)+len(missingSKUOffers))
	for _, group := range groupsBySKU {
		sort.SliceStable(group.Offers, func(i, j int) bool {
			return lessProduct(group.Offers[i], group.Offers[j])
		})
		groups = append(groups, *group)
	}
	for _, product := range missingSKUOffers {
		groups = append(groups, ProductGroup{Offers: []ProductMetrics{product}})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].SellerSKU != groups[j].SellerSKU {
			return groups[i].SellerSKU < groups[j].SellerSKU
		}
		return lessProduct(groups[i].Offers[0], groups[j].Offers[0])
	})

	warnings := make([]MergeWarning, 0, len(missingSKUOffers)+len(counts))
	for _, product := range missingSKUOffers {
		warnings = append(warnings, MergeWarning{
			Marketplace: product.Marketplace,
			ProductIDs:  []string{product.ProductID},
			Reason:      "missing_seller_sku",
		})
	}
	for key, productIDs := range counts {
		if len(productIDs) > 1 {
			ids := make([]string, 0, len(productIDs))
			for productID := range productIDs {
				ids = append(ids, productID)
			}
			sort.Strings(ids)
			warnings = append(warnings, MergeWarning{
				SellerSKU:   key.sku,
				Marketplace: key.marketplace,
				ProductIDs:  ids,
				Reason:      "duplicate_seller_sku_in_marketplace",
			})
		}
	}
	sort.SliceStable(warnings, func(i, j int) bool {
		if warnings[i].SellerSKU != warnings[j].SellerSKU {
			return warnings[i].SellerSKU < warnings[j].SellerSKU
		}
		if warnings[i].Marketplace != warnings[j].Marketplace {
			return warnings[i].Marketplace < warnings[j].Marketplace
		}
		return warnings[i].Reason < warnings[j].Reason
	})
	return groups, warnings
}

func lessProduct(left, right ProductMetrics) bool {
	if left.Marketplace != right.Marketplace {
		return left.Marketplace < right.Marketplace
	}
	if left.ProductID != right.ProductID {
		return left.ProductID < right.ProductID
	}
	return left.VariantID < right.VariantID
}
