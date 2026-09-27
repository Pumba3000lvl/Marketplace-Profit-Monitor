package ozon

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Product describes a seller product returned by the product-list operation.
type Product struct {
	ProductID      string `json:"product_id"`
	OfferID        string `json:"offer_id"`
	IsArchived     bool   `json:"is_archived"`
	IsAutoArchived bool   `json:"is_autoarchived"`
}

func (product *Product) UnmarshalJSON(data []byte) error {
	var raw struct {
		ProductID      json.RawMessage `json:"product_id"`
		OfferID        string          `json:"offer_id"`
		IsArchived     bool            `json:"is_archived"`
		IsAutoArchived bool            `json:"is_autoarchived"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	productID, err := decodeAmount(raw.ProductID)
	if err != nil {
		return fmt.Errorf("decode product_id: %w", err)
	}
	product.ProductID = productID
	product.OfferID = raw.OfferID
	product.IsArchived = raw.IsArchived
	product.IsAutoArchived = raw.IsAutoArchived
	return nil
}

// PriceItem contains a product's current Ozon prices. Monetary amounts are
// strings, matching Ozon's decimal-string API representation.
type PriceItem struct {
	ProductID    string `json:"product_id,omitempty"`
	OfferID      string `json:"offer_id"`
	Price        string `json:"price"`
	OldPrice     string `json:"old_price"`
	MinPrice     string `json:"min_price"`
	NetPrice     string `json:"net_price"`
	CurrencyCode string `json:"currency_code"`
}

// UnmarshalJSON accepts Ozon's nested price object as well as a flat price
// payload, which keeps the public PriceItem useful for both representations.
func (item *PriceItem) UnmarshalJSON(data []byte) error {
	var raw struct {
		ProductID    json.RawMessage `json:"product_id"`
		OfferID      string          `json:"offer_id"`
		Price        json.RawMessage `json:"price"`
		OldPrice     json.RawMessage `json:"old_price"`
		MinPrice     json.RawMessage `json:"min_price"`
		NetPrice     json.RawMessage `json:"net_price"`
		CurrencyCode string          `json:"currency_code"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	productID, err := decodeAmount(raw.ProductID)
	if err != nil {
		return fmt.Errorf("decode product_id: %w", err)
	}
	item.ProductID = productID
	item.OfferID = raw.OfferID
	item.CurrencyCode = raw.CurrencyCode

	if len(raw.Price) > 0 && raw.Price[0] == '{' {
		var nested struct {
			Price        json.RawMessage `json:"price"`
			OldPrice     json.RawMessage `json:"old_price"`
			MinPrice     json.RawMessage `json:"min_price"`
			NetPrice     json.RawMessage `json:"net_price"`
			CurrencyCode string          `json:"currency_code"`
		}
		if err := json.Unmarshal(raw.Price, &nested); err != nil {
			return err
		}
		if item.Price, err = decodeAmount(nested.Price); err != nil {
			return fmt.Errorf("decode nested price: %w", err)
		}
		if item.OldPrice, err = decodeAmount(nested.OldPrice); err != nil {
			return fmt.Errorf("decode nested old_price: %w", err)
		}
		if item.MinPrice, err = decodeAmount(nested.MinPrice); err != nil {
			return fmt.Errorf("decode nested min_price: %w", err)
		}
		if item.NetPrice, err = decodeAmount(nested.NetPrice); err != nil {
			return fmt.Errorf("decode nested net_price: %w", err)
		}
		if item.CurrencyCode == "" {
			item.CurrencyCode = nested.CurrencyCode
		}
		return nil
	}

	if item.Price, err = decodeAmount(raw.Price); err != nil {
		return fmt.Errorf("decode price: %w", err)
	}
	if item.OldPrice, err = decodeAmount(raw.OldPrice); err != nil {
		return fmt.Errorf("decode old_price: %w", err)
	}
	if item.MinPrice, err = decodeAmount(raw.MinPrice); err != nil {
		return fmt.Errorf("decode min_price: %w", err)
	}
	if item.NetPrice, err = decodeAmount(raw.NetPrice); err != nil {
		return fmt.Errorf("decode net_price: %w", err)
	}
	return nil
}

func decodeAmount(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if json.Valid(trimmed) && len(trimmed) > 0 && (trimmed[0] == '-' || (trimmed[0] >= '0' && trimmed[0] <= '9')) {
		return string(trimmed), nil
	}
	return "", fmt.Errorf("invalid JSON amount %s", raw)
}

// AnalyticsDimension is one dimension value in an analytics row.
type AnalyticsDimension struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (dimension *AnalyticsDimension) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID   json.RawMessage `json:"id"`
		Name string          `json:"name"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	id, err := decodeAmount(raw.ID)
	if err != nil {
		return fmt.Errorf("decode analytics dimension id: %w", err)
	}
	dimension.ID = id
	dimension.Name = raw.Name
	return nil
}

// AnalyticsRow contains the dimensions and metric values for one analytics result.
type AnalyticsRow struct {
	Dimensions []AnalyticsDimension `json:"dimensions"`
	Metrics    []float64            `json:"metrics"`
}

type productListResponse struct {
	Result struct {
		Items  []Product `json:"items"`
		LastID string    `json:"last_id"`
	} `json:"result"`
}

type pricesResponse struct {
	Items  []PriceItem `json:"items"`
	Cursor string      `json:"cursor"`
	Total  int         `json:"total"`
	Result struct {
		Items  []PriceItem `json:"items"`
		Cursor string      `json:"cursor"`
		Total  int         `json:"total"`
	} `json:"result"`
}

func (r pricesResponse) items() []PriceItem {
	if r.Items != nil {
		return r.Items
	}
	return r.Result.Items
}

func (r pricesResponse) nextCursor() string {
	if r.Cursor != "" {
		return r.Cursor
	}
	return r.Result.Cursor
}

func (r pricesResponse) total() int {
	if r.Total != 0 {
		return r.Total
	}
	return r.Result.Total
}

type analyticsResponse struct {
	Result struct {
		Data      []AnalyticsRow `json:"data"`
		Total     int            `json:"total"`
		Totals    []float64      `json:"totals"`
		Timestamp string         `json:"timestamp"`
	} `json:"result"`
}
