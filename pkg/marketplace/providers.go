package marketplace

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pumba3000lvl/marketplace-profit-monitor/pkg/ozon"
	"github.com/pumba3000lvl/marketplace-profit-monitor/pkg/wildberries"
)

const pageSize = 1000

type WildberriesAPI interface {
	GetProducts(context.Context, int, int) ([]wildberries.Product, error)
	GetCommissions(context.Context, string) ([]wildberries.CommissionItem, error)
}

type WildberriesProvider struct {
	client WildberriesAPI
}

func NewWildberriesProvider(client WildberriesAPI) *WildberriesProvider {
	return &WildberriesProvider{client: client}
}

func (*WildberriesProvider) Marketplace() Marketplace { return Wildberries }

func (p *WildberriesProvider) GetProductMetrics(ctx context.Context) ([]ProductMetrics, error) {
	if p.client == nil {
		return nil, errors.New("Wildberries client is not configured")
	}
	var products []wildberries.Product
	var productErr error
	for offset := 0; ; offset += pageSize {
		page, err := p.client.GetProducts(ctx, pageSize, offset)
		if err != nil {
			productErr = fmt.Errorf("get Wildberries products: %w", err)
			break
		}
		products = append(products, page...)
		if len(page) < pageSize {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if len(products) == 0 && productErr != nil {
		return nil, productErr
	}

	commissions, commissionErr := p.client.GetCommissions(ctx, "ru")
	rates := make(map[int64]float64, len(commissions))
	for _, commission := range commissions {
		if commission.SubjectID > 0 {
			rates[commission.SubjectID] = commission.KgvpSupplier
		}
	}
	updatedAt := time.Now().UTC()
	metrics := make([]ProductMetrics, 0, len(products))
	var mappingErrors []error
	for _, product := range products {
		if len(product.Sizes) == 0 {
			metric, err := wildberriesMetric(product, wildberries.ProductSize{}, rates, updatedAt)
			metrics = append(metrics, metric)
			if err != nil {
				mappingErrors = append(mappingErrors, err)
			}
			continue
		}
		for _, size := range product.Sizes {
			metric, err := wildberriesMetric(product, size, rates, updatedAt)
			metrics = append(metrics, metric)
			if err != nil {
				mappingErrors = append(mappingErrors, err)
			}
		}
	}
	if productErr != nil {
		mappingErrors = append(mappingErrors, productErr)
	}
	if commissionErr != nil {
		mappingErrors = append(mappingErrors, fmt.Errorf("get Wildberries commissions: %w", commissionErr))
	}
	return metrics, errors.Join(mappingErrors...)
}

func wildberriesMetric(product wildberries.Product, size wildberries.ProductSize, rates map[int64]float64, updatedAt time.Time) (ProductMetrics, error) {
	metric := ProductMetrics{
		Marketplace: Wildberries,
		ProductID:   strconv.FormatInt(product.NmID, 10),
		SellerSKU:   product.VendorCode,
		VariantID:   strconv.FormatInt(size.SizeID, 10),
		VariantName: size.TechSizeName,
		UpdatedAt:   updatedAt,
	}
	var metricErrors []error
	currency := strings.ToUpper(strings.TrimSpace(product.CurrencyIsoCode4217))
	if currency == "643" || currency == "RUB" {
		price := size.DiscountedPrice
		if price <= 0 && size.Price > 0 {
			price = float64(size.Price)
		}
		if price > 0 {
			if kopecks, err := parseRubles(strconv.FormatFloat(price, 'f', -1, 64)); err == nil {
				metric.CurrentPrice = rubles(kopecks)
			} else {
				metricErrors = append(metricErrors, fmt.Errorf("parse Wildberries price for product %q: %w", metric.ProductID, err))
			}
		}
	}
	if rate, exists := rates[product.SubjectID]; exists {
		rateValue := rate
		metric.CommissionRatePercent = &rateValue
		if metric.CurrentPrice != nil {
			priceKopecks, err := parseRubles(strconv.FormatFloat(*metric.CurrentPrice, 'f', 2, 64))
			if err != nil {
				metricErrors = append(metricErrors, fmt.Errorf("parse Wildberries sale price for commission: %w", err))
			} else if amount, err := commissionAmount(priceKopecks, rate); err != nil {
				metricErrors = append(metricErrors, fmt.Errorf("calculate Wildberries commission for product %q: %w", metric.ProductID, err))
			} else {
				metric.Commission = rubles(*amount)
			}
		}
	}
	return metric, errors.Join(metricErrors...)
}

type OzonAPI interface {
	GetProductList(context.Context, int, string) ([]ozon.Product, error)
	GetPrices(context.Context, []string) ([]ozon.PriceItem, error)
}

type OzonProvider struct {
	client OzonAPI
}

func NewOzonProvider(client OzonAPI) *OzonProvider {
	return &OzonProvider{client: client}
}

func (*OzonProvider) Marketplace() Marketplace { return Ozon }

func (p *OzonProvider) GetProductMetrics(ctx context.Context) ([]ProductMetrics, error) {
	if p.client == nil {
		return nil, errors.New("Ozon client is not configured")
	}
	products, err := p.client.GetProductList(ctx, pageSize, "")
	if err != nil {
		return nil, fmt.Errorf("get Ozon products: %w", err)
	}
	metrics := make([]ProductMetrics, len(products))
	productIDs := make([]string, 0, len(products))
	updatedAt := time.Now().UTC()
	for index, product := range products {
		metrics[index] = ProductMetrics{
			Marketplace:          Ozon,
			ProductID:            product.OfferID,
			MarketplaceProductID: product.ProductID,
			SellerSKU:            product.OfferID,
			UpdatedAt:            updatedAt,
		}
		if strings.TrimSpace(product.ProductID) != "" {
			productIDs = append(productIDs, product.ProductID)
		}
	}
	if len(productIDs) == 0 {
		return metrics, nil
	}
	prices, err := p.client.GetPrices(ctx, productIDs)
	if err != nil {
		return metrics, fmt.Errorf("get Ozon prices: %w", err)
	}
	byProductID := make(map[string]ozon.PriceItem, len(prices))
	for _, price := range prices {
		byProductID[price.ProductID] = price
	}
	var parseErrors []error
	for index := range metrics {
		price, exists := byProductID[metrics[index].MarketplaceProductID]
		if !exists || !strings.EqualFold(strings.TrimSpace(price.CurrencyCode), "RUB") || strings.TrimSpace(price.Price) == "" {
			continue
		}
		kopecks, err := parseRubles(price.Price)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Errorf("parse Ozon price for product %q: %w", metrics[index].MarketplaceProductID, err))
			continue
		}
		metrics[index].CurrentPrice = rubles(kopecks)
	}
	return metrics, errors.Join(parseErrors...)
}
