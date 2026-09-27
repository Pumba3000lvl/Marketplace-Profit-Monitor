package wildberries

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// GetCommissions returns commission rates, optionally localized as ru, en, or zh.
func (c *Client) GetCommissions(ctx context.Context, locale string) ([]CommissionItem, error) {
	switch locale {
	case "", "ru", "en", "zh":
	default:
		return nil, fmt.Errorf("unsupported Wildberries locale %q: use ru, en, or zh", locale)
	}

	query := make(url.Values)
	if locale != "" {
		query.Set("locale", locale)
	}
	var response commissionsResponse
	if err := c.getJSON(ctx, c.commonBaseURL, "/api/v1/tariffs/commission", query, &response); err != nil {
		return nil, err
	}
	if response.Report == nil {
		return []CommissionItem{}, nil
	}
	return response.Report, nil
}

// GetProducts returns a page of the seller's current products and prices.
// Wildberries accepts limits from 1 through 1000 and non-negative offsets.
func (c *Client) GetProducts(ctx context.Context, limit, offset int) ([]Product, error) {
	if limit < 1 || limit > maxPageSize {
		return nil, fmt.Errorf("product limit must be between 1 and %d", maxPageSize)
	}
	if offset < 0 {
		return nil, errors.New("product offset cannot be negative")
	}

	query := make(url.Values)
	query.Set("limit", strconv.Itoa(limit))
	query.Set("offset", strconv.Itoa(offset))
	var response productsResponse
	if err := c.getJSON(ctx, c.pricesBaseURL, "/api/v2/list/goods/filter", query, &response); err != nil {
		return nil, err
	}
	if err := checkAPIEnvelope(apiResponseError{Error: response.Error, ErrorText: response.ErrorText}); err != nil {
		return nil, err
	}
	if response.Data.ListGoods == nil {
		return []Product{}, nil
	}
	return response.Data.ListGoods, nil
}

// GetUploadTask retrieves the status and timing metadata for a processed upload.
// The API requires an uploadID; it does not provide a task-list endpoint.
func (c *Client) GetUploadTask(ctx context.Context, uploadID int64) (UploadTask, error) {
	if uploadID <= 0 {
		return UploadTask{}, errors.New("uploadID must be positive")
	}
	query := make(url.Values)
	query.Set("uploadID", strconv.FormatInt(uploadID, 10))
	var response uploadTaskResponse
	if err := c.getJSON(ctx, c.pricesBaseURL, "/api/v2/history/tasks", query, &response); err != nil {
		return UploadTask{}, err
	}
	if err := checkAPIEnvelope(apiResponseError{Error: response.Error, ErrorText: response.ErrorText}); err != nil {
		return UploadTask{}, err
	}
	if response.Data == nil {
		return UploadTask{}, errors.New("Wildberries API returned no upload task data")
	}
	if response.Data.UploadID != 0 && response.Data.UploadID != uploadID {
		return UploadTask{}, fmt.Errorf("Wildberries API returned uploadID %d for requested uploadID %d", response.Data.UploadID, uploadID)
	}
	return *response.Data, nil
}

// GetUploadTaskDetails retrieves all product/size entries for a processed upload.
func (c *Client) GetUploadTaskDetails(ctx context.Context, uploadID int64) ([]UploadTaskProduct, error) {
	if uploadID <= 0 {
		return nil, errors.New("uploadID must be positive")
	}
	if c.pageSize < 1 || c.pageSize > maxPageSize {
		return nil, fmt.Errorf("history page size must be between 1 and %d", maxPageSize)
	}

	var products []UploadTaskProduct
	for offset := 0; ; offset += c.pageSize {
		query := make(url.Values)
		query.Set("uploadID", strconv.FormatInt(uploadID, 10))
		query.Set("limit", strconv.Itoa(c.pageSize))
		query.Set("offset", strconv.Itoa(offset))
		var response uploadTaskDetailsResponse
		if err := c.getJSON(ctx, c.pricesBaseURL, "/api/v2/history/goods/task", query, &response); err != nil {
			return nil, err
		}
		if err := checkAPIEnvelope(apiResponseError{Error: response.Error, ErrorText: response.ErrorText}); err != nil {
			return nil, err
		}
		if response.Data == nil {
			break
		}
		if response.Data.UploadID != nil && *response.Data.UploadID != uploadID {
			return nil, fmt.Errorf("Wildberries API returned uploadID %d for requested uploadID %d", *response.Data.UploadID, uploadID)
		}
		page := response.Data.HistoryGoods
		products = append(products, page...)
		if len(page) < c.pageSize {
			break
		}
	}
	if products == nil {
		return []UploadTaskProduct{}, nil
	}
	return products, nil
}

// GetPriceHistory returns processed API-upload price points in the inclusive
// date range. Wildberries currently requires uploadID for both history endpoints
// and exposes no endpoint to enumerate upload IDs, so a complete range cannot be
// discovered from nmID and dates alone. Use GetPriceHistoryForUploads if the
// upload IDs are available from the caller's own records.
func (c *Client) GetPriceHistory(ctx context.Context, nmID int64, dateFrom, dateTo time.Time) ([]PricePoint, error) {
	if err := validateHistoryRange(nmID, dateFrom, dateTo); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%w: GET /api/v2/history/tasks and GET /api/v2/history/goods/task both require uploadID; no supported operation lists upload IDs", ErrUploadTaskEnumerationUnsupported)
}

// GetPriceHistoryForUploads filters processed API-upload details for known
// upload IDs. It cannot include changes made manually in the seller cabinet.
func (c *Client) GetPriceHistoryForUploads(ctx context.Context, nmID int64, dateFrom, dateTo time.Time, uploadIDs []int64) ([]PricePoint, error) {
	if err := validateHistoryRange(nmID, dateFrom, dateTo); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(uploadIDs) == 0 {
		return []PricePoint{}, nil
	}

	seen := make(map[int64]struct{}, len(uploadIDs))
	var points []PricePoint
	for _, uploadID := range uploadIDs {
		if _, exists := seen[uploadID]; exists {
			continue
		}
		seen[uploadID] = struct{}{}

		task, err := c.GetUploadTask(ctx, uploadID)
		if err != nil {
			return nil, fmt.Errorf("get upload task %d: %w", uploadID, err)
		}
		if task.Status != 3 && task.Status != 5 {
			continue
		}
		if task.ActivationDate == nil || task.ActivationDate.Before(dateFrom) || task.ActivationDate.After(dateTo) {
			continue
		}
		products, err := c.GetUploadTaskDetails(ctx, uploadID)
		if err != nil {
			return nil, fmt.Errorf("get upload task %d details: %w", uploadID, err)
		}
		points = append(points, pricePointsForTask(nmID, uploadID, *task.ActivationDate, products)...)
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].Timestamp.Equal(points[j].Timestamp) {
			if points[i].UploadID == points[j].UploadID {
				return sizeID(points[i].SizeID) < sizeID(points[j].SizeID)
			}
			return points[i].UploadID < points[j].UploadID
		}
		return points[i].Timestamp.Before(points[j].Timestamp)
	})
	if points == nil {
		return []PricePoint{}, nil
	}
	return points, nil
}

func validateHistoryRange(nmID int64, dateFrom, dateTo time.Time) error {
	if nmID <= 0 {
		return errors.New("nmID must be positive")
	}
	if dateFrom.IsZero() || dateTo.IsZero() {
		return errors.New("dateFrom and dateTo must be set")
	}
	if dateTo.Before(dateFrom) {
		return errors.New("dateTo must be on or after dateFrom")
	}
	return nil
}

func pricePointsForTask(nmID, uploadID int64, timestamp time.Time, items []UploadTaskProduct) []PricePoint {
	var points []PricePoint
	for _, item := range items {
		if item.NmID != nmID || item.Status != 2 || item.Price == nil {
			continue
		}
		points = append(points, PricePoint{
			NmID:                item.NmID,
			UploadID:            uploadID,
			Timestamp:           timestamp,
			SizeID:              item.SizeID,
			TechSizeName:        item.TechSizeName,
			Price:               *item.Price,
			CurrencyIsoCode4217: item.CurrencyIsoCode4217,
			Discount:            item.Discount,
			ClubDiscount:        item.ClubDiscount,
		})
	}
	return points
}

func sizeID(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
