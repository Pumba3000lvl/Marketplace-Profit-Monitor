package ozon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(server *httptest.Server) *Client {
	client := NewClient("seller-client-id", "seller-api-key")
	client.baseURL = server.URL
	return client
}

func TestNewClientAndProductListPagination(t *testing.T) {
	var calls int
	var logOutput bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/v3/product/list" {
			t.Errorf("request = %s %s, want POST /v3/product/list", r.Method, r.URL.Path)
		}
		if r.Header.Get("Client-Id") != "seller-client-id" || r.Header.Get("Api-Key") != "seller-api-key" {
			t.Errorf("unexpected Ozon credentials headers")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		var body struct {
			Filter struct {
				Visibility string `json:"visibility"`
			} `json:"filter"`
			LastID string `json:"last_id"`
			Limit  int    `json:"limit"`
		}
		if err := jsonNewDecoder(r).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Filter.Visibility != "ALL" || body.Limit != 2 {
			t.Errorf("request body = %#v, want visibility ALL and limit 2", body)
		}
		switch calls {
		case 1:
			if body.LastID != "" {
				t.Errorf("first last_id = %q, want empty", body.LastID)
			}
			w.Header().Set("X-Ozon-Request-Id", "ozon-req-1")
			_, _ = w.Write([]byte(`{"result":{"items":[{"product_id":123,"offer_id":"SKU-1"}],"last_id":"next"}}`))
		case 2:
			if body.LastID != "next" {
				t.Errorf("second last_id = %q, want next", body.LastID)
			}
			_, _ = w.Write([]byte(`{"result":{"items":[{"product_id":"456","offer_id":"SKU-2","is_archived":true}],"last_id":""}}`))
		}
	}))
	defer server.Close()

	client := testClient(server)
	client.logger = log.New(&logOutput, "", 0)
	if client.httpClient.Timeout != 30*time.Second {
		t.Fatalf("timeout = %s, want 30s", client.httpClient.Timeout)
	}
	products, err := client.GetProductList(context.Background(), 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(products) != 2 || products[0].ProductID != "123" || products[1].OfferID != "SKU-2" ||
		!products[1].IsArchived {
		t.Fatalf("products = %#v, calls = %d", products, calls)
	}
	if !strings.Contains(logOutput.String(), `request_id="ozon-req-1"`) {
		t.Fatalf("request ID missing from safe request log: %s", logOutput.String())
	}
	if strings.Contains(logOutput.String(), "seller-api-key") || strings.Contains(logOutput.String(), "seller-client-id") {
		t.Fatalf("request logs leaked credentials: %s", logOutput.String())
	}
}

func TestGetPricesUsesCursorAndDecodesNestedDecimalAmounts(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v5/product/info/prices" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s, want POST /v5/product/info/prices", r.Method, r.URL.Path)
		}
		var body struct {
			Cursor string `json:"cursor"`
			Filter struct {
				ProductID  []string `json:"product_id"`
				Visibility string   `json:"visibility"`
			} `json:"filter"`
			Limit int `json:"limit"`
		}
		if err := jsonNewDecoder(r).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if strings.Join(body.Filter.ProductID, ",") != "123,456" || body.Filter.Visibility != "ALL" || body.Limit != 1000 {
			t.Errorf("request body = %#v", body)
		}
		if calls == 1 {
			if body.Cursor != "" {
				t.Errorf("initial cursor = %q, want empty", body.Cursor)
			}
			_, _ = w.Write([]byte(`{"items":[{"product_id":123,"offer_id":"SKU-1","price":{"price":"99.50","old_price":"110.00","min_price":"90.00","net_price":"80.00","currency_code":"RUB"}}],"cursor":"next","total":2}`))
			return
		}
		if body.Cursor != "next" {
			t.Errorf("next cursor = %q, want next", body.Cursor)
		}
		_, _ = w.Write([]byte(`{"items":[{"product_id":"456","offer_id":"SKU-2","price":"45.00","old_price":"50.00","min_price":"40.00","net_price":"35.00","currency_code":"RUB"}],"cursor":"","total":2}`))
	}))
	defer server.Close()

	prices, err := testClient(server).GetPrices(context.Background(), []string{"123", "123", "456"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(prices) != 2 {
		t.Fatalf("prices = %#v, calls = %d", prices, calls)
	}
	got := prices[0]
	if got.ProductID != "123" || got.OfferID != "SKU-1" || got.Price != "99.50" || got.OldPrice != "110.00" ||
		got.MinPrice != "90.00" || got.NetPrice != "80.00" || got.CurrencyCode != "RUB" {
		t.Fatalf("unexpected nested price item: %#v", got)
	}
	if prices[1].Price != "45.00" || prices[1].CurrencyCode != "RUB" {
		t.Fatalf("unexpected flat price item: %#v", prices[1])
	}
}

func TestGetAnalyticsBuildsDailyRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/analytics/data" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s, want POST /v1/analytics/data", r.Method, r.URL.Path)
		}
		var body struct {
			DateFrom  string   `json:"date_from"`
			DateTo    string   `json:"date_to"`
			Metrics   []string `json:"metrics"`
			Dimension []string `json:"dimension"`
			Filters   []any    `json:"filters"`
			Limit     int      `json:"limit"`
			Offset    int      `json:"offset"`
		}
		if err := jsonNewDecoder(r).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.DateFrom != "2026-01-01" || body.DateTo != "2026-01-02" ||
			strings.Join(body.Metrics, ",") != "revenue,ordered_units" ||
			strings.Join(body.Dimension, ",") != "day" || body.Filters == nil ||
			body.Limit != 1000 || body.Offset != 0 {
			t.Errorf("request body = %#v", body)
		}
		_, _ = w.Write([]byte(`{"result":{"data":[{"dimensions":[{"id":"2026-01-01","name":"2026-01-01"}],"metrics":[100.5,2]}],"total":1}}`))
	}))
	defer server.Close()

	rows, err := testClient(server).GetAnalytics(context.Background(), "2026-01-01", "2026-01-02", []string{"revenue", "ordered_units"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || len(rows[0].Dimensions) != 1 || rows[0].Dimensions[0].ID != "2026-01-01" ||
		len(rows[0].Metrics) != 2 || rows[0].Metrics[0] != 100.5 || rows[0].Metrics[1] != 2 {
		t.Fatalf("unexpected analytics rows: %#v", rows)
	}
}

func TestAPIErrorIncludesOzonCodeAndRequestID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Ozon-Request-Id", "req-error-1")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":7,"message":"invalid product selection"}`))
	}))
	defer server.Close()

	_, err := testClient(server).GetPrices(context.Background(), []string{"123"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest || apiErr.Code != "7" ||
		apiErr.Message != "invalid product selection" || apiErr.RequestID != "req-error-1" {
		t.Fatalf("unexpected API error: %#v", apiErr)
	}
}

func TestAPIErrorEnvelopeOnHTTP200AndRedaction(t *testing.T) {
	const secret = "seller-api-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"code":"AUTH_ERROR","message":"bad key %s","request_id":"body-request-id"}`, secret)
	}))
	defer server.Close()

	_, err := testClient(server).GetPrices(context.Background(), []string{"123"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusOK ||
		apiErr.Code != "AUTH_ERROR" || apiErr.RequestID != "body-request-id" ||
		strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("unexpected application API error: %#v (%v)", apiErr, err)
	}
}

func TestRejectsInvalidInputsAndEmptyPricesAvoidRequest(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := testClient(server)

	if _, err := client.GetProductList(context.Background(), 0, ""); err == nil {
		t.Fatal("GetProductList accepted a non-positive limit")
	}
	if _, err := client.GetProductList(context.Background(), 1001, ""); err == nil {
		t.Fatal("GetProductList accepted a limit above the API maximum")
	}
	if _, err := client.GetPrices(context.Background(), []string{" "}); err == nil {
		t.Fatal("GetPrices accepted an empty product ID")
	}
	if _, err := client.GetAnalytics(context.Background(), "", "2026-01-02", []string{"revenue"}); err == nil {
		t.Fatal("GetAnalytics accepted an empty date")
	}
	if _, err := client.GetAnalytics(context.Background(), "2026-01-01", "2026-01-02", nil); err == nil {
		t.Fatal("GetAnalytics accepted no metrics")
	}
	prices, err := client.GetPrices(context.Background(), nil)
	if err != nil || prices == nil || calls != 0 {
		t.Fatalf("empty GetPrices = %#v, %v; requests = %d", prices, err, calls)
	}
}

func TestContextCancellationPreventsRequest(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := testClient(server).GetProductList(ctx, 10, "")
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("error = %v, calls = %d; want canceled without request", err, calls)
	}
}

// Kept as a helper to make request decoding assertions concise.
func jsonNewDecoder(r *http.Request) *json.Decoder { return json.NewDecoder(r.Body) }
