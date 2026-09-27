package ozon

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type productListRequest struct {
	Filter struct {
		OfferID    []string `json:"offer_id"`
		ProductID  []string `json:"product_id"`
		Visibility string   `json:"visibility"`
	} `json:"filter"`
	LastID string `json:"last_id"`
	Limit  int    `json:"limit"`
}

// GetProductList returns all products from the supplied continuation point.
// Ozon's last_id cursor is followed internally until the API reports no next page.
func (c *Client) GetProductList(ctx context.Context, limit int, lastID string) ([]Product, error) {
	if limit < 1 || limit > maxPageSize {
		return nil, fmt.Errorf("product limit must be between 1 and %d", maxPageSize)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var products []Product
	seenCursors := make(map[string]struct{})
	cursor := lastID
	for {
		requestBody := productListRequest{LastID: cursor, Limit: limit}
		requestBody.Filter.OfferID = []string{}
		requestBody.Filter.ProductID = []string{}
		requestBody.Filter.Visibility = "ALL"
		var response productListResponse
		if err := c.postJSON(ctx, "/v3/product/list", requestBody, &response); err != nil {
			return nil, err
		}
		products = append(products, response.Result.Items...)
		next := strings.TrimSpace(response.Result.LastID)
		if next == "" {
			break
		}
		if next == cursor {
			return nil, errors.New("Ozon product list returned a repeated last_id cursor")
		}
		if _, exists := seenCursors[next]; exists {
			return nil, errors.New("Ozon product list returned a repeated last_id cursor")
		}
		seenCursors[next] = struct{}{}
		cursor = next
	}
	if products == nil {
		return []Product{}, nil
	}
	return products, nil
}

type pricesRequest struct {
	Cursor string `json:"cursor"`
	Filter struct {
		OfferID    []string `json:"offer_id"`
		ProductID  []string `json:"product_id"`
		Visibility string   `json:"visibility"`
	} `json:"filter"`
	Limit int `json:"limit"`
}

// GetPrices returns prices for the requested product IDs, following Ozon's
// cursor and splitting larger selections into API-sized batches.
func (c *Client) GetPrices(ctx context.Context, productIDs []string) ([]PriceItem, error) {
	ids, err := uniqueProductIDs(productIDs)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []PriceItem{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var prices []PriceItem
	for start := 0; start < len(ids); start += maxPageSize {
		end := start + maxPageSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		cursor := ""
		batchCount := 0
		seenCursors := make(map[string]struct{})
		for {
			requestBody := pricesRequest{Cursor: cursor, Limit: maxPageSize}
			requestBody.Filter.OfferID = []string{}
			requestBody.Filter.ProductID = batch
			requestBody.Filter.Visibility = "ALL"
			var response pricesResponse
			if err := c.postJSON(ctx, "/v5/product/info/prices", requestBody, &response); err != nil {
				return nil, err
			}
			items := response.items()
			prices = append(prices, items...)
			batchCount += len(items)
			next := strings.TrimSpace(response.nextCursor())
			if next == "" || len(items) == 0 ||
				(response.total() > 0 && batchCount >= response.total()) {
				break
			}
			if next == cursor {
				return nil, errors.New("Ozon prices returned a repeated cursor")
			}
			if _, exists := seenCursors[next]; exists {
				return nil, errors.New("Ozon prices returned a repeated cursor")
			}
			seenCursors[next] = struct{}{}
			cursor = next
		}
	}
	if prices == nil {
		return []PriceItem{}, nil
	}
	return prices, nil
}

func uniqueProductIDs(productIDs []string) ([]string, error) {
	ids := make([]string, 0, len(productIDs))
	seen := make(map[string]struct{}, len(productIDs))
	for _, id := range productIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, errors.New("product IDs cannot be empty")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

type analyticsRequest struct {
	DateFrom  string   `json:"date_from"`
	DateTo    string   `json:"date_to"`
	Metrics   []string `json:"metrics"`
	Dimension []string `json:"dimension"`
	Filters   []any    `json:"filters"`
	Limit     int      `json:"limit"`
	Offset    int      `json:"offset"`
}

// GetAnalytics returns all daily analytics rows in the requested date range.
// The API requires a dimension; this method uses "day" since no dimension is
// included in its public signature.
func (c *Client) GetAnalytics(ctx context.Context, dateFrom, dateTo string, metrics []string) ([]AnalyticsRow, error) {
	if strings.TrimSpace(dateFrom) == "" || strings.TrimSpace(dateTo) == "" {
		return nil, errors.New("dateFrom and dateTo must not be empty")
	}
	if len(metrics) == 0 {
		return nil, errors.New("at least one analytics metric is required")
	}
	for _, metric := range metrics {
		if strings.TrimSpace(metric) == "" {
			return nil, errors.New("analytics metrics cannot be empty")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var rows []AnalyticsRow
	for offset := 0; ; {
		requestBody := analyticsRequest{
			DateFrom:  dateFrom,
			DateTo:    dateTo,
			Metrics:   metrics,
			Dimension: []string{"day"},
			Filters:   []any{},
			Limit:     maxPageSize,
			Offset:    offset,
		}
		var response analyticsResponse
		if err := c.postJSON(ctx, "/v1/analytics/data", requestBody, &response); err != nil {
			return nil, err
		}
		page := response.Result.Data
		rows = append(rows, page...)
		offset += len(page)
		if len(page) == 0 || len(page) < maxPageSize ||
			(response.Result.Total > 0 && offset >= response.Result.Total) {
			break
		}
	}
	if rows == nil {
		return []AnalyticsRow{}, nil
	}
	return rows, nil
}
