package wildberries

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(server *httptest.Server, apiKey string) *Client {
	client := NewClient(apiKey)
	client.commonBaseURL = server.URL
	client.pricesBaseURL = server.URL
	return client
}

func TestNewClientAndRequestLoggingDoNotExposeAPIKey(t *testing.T) {
	const token = "seller-secret-token"
	var logOutput bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != token {
			t.Errorf("Authorization = %q, want configured token", got)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		_, _ = w.Write([]byte(`{"data":{"listGoods":[]}}`))
	}))
	defer server.Close()

	client := testClient(server, token)
	client.logger = log.New(&logOutput, "", 0)
	if client.httpClient.Timeout != 30*time.Second {
		t.Fatalf("HTTP timeout = %s, want 30s", client.httpClient.Timeout)
	}
	if _, err := client.GetProducts(context.Background(), 10, 0); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logOutput.String(), token) {
		t.Fatalf("request logs leaked API key: %s", logOutput.String())
	}
	if !strings.Contains(logOutput.String(), "method=GET path=/api/v2/list/goods/filter status=200") {
		t.Fatalf("request log missing safe request metadata: %s", logOutput.String())
	}
}

func TestGetCommissionsAndProducts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/tariffs/commission":
			if got := r.URL.Query().Get("locale"); got != "en" {
				t.Errorf("locale = %q, want en", got)
			}
			_, _ = w.Write([]byte(`{"report":[{"parentID":657,"parentName":"Appliances","subjectID":9,"subjectName":"Item","kgvpMarketplace":15.5,"kgvpSupplier":12.5,"paidStorageKgvp":14}]}`))
		case "/api/v2/list/goods/filter":
			if got := r.URL.Query(); got.Get("limit") != "25" || got.Get("offset") != "50" {
				t.Errorf("query = %v, want limit=25 and offset=50", got)
			}
			_, _ = w.Write([]byte(`{"data":{"listGoods":[{"nmID":101,"vendorCode":"SKU-1","sizes":[{"sizeID":123,"price":1000,"discountedPrice":800,"clubDiscountedPrice":760,"techSizeName":"M"}],"currencyIsoCode4217":"RUB","discount":20,"clubDiscount":5,"editableSizePrice":true,"wholesaleDiscountThreshold":[{"minQuantity":10,"wholesaleDiscount":7,"level":1}]}]},"error":false,"errorText":""}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := testClient(server, "key")

	commissions, err := client.GetCommissions(context.Background(), "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(commissions) != 1 || commissions[0].ParentID != 657 || commissions[0].KgvpMarketplace != 15.5 ||
		commissions[0].KgvpSupplier != 12.5 || commissions[0].PaidStorageKgvp != 14 {
		t.Fatalf("unexpected commissions: %#v", commissions)
	}

	products, err := client.GetProducts(context.Background(), 25, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 || products[0].NmID != 101 || products[0].Sizes[0].Price != 1000 ||
		products[0].WholesaleDiscountLevels[0].WholesaleDiscount != 7 {
		t.Fatalf("unexpected products: %#v", products)
	}
}

func TestRejectsInvalidInputs(t *testing.T) {
	client := NewClient("key")
	if _, err := client.GetCommissions(context.Background(), "fr"); err == nil {
		t.Fatal("GetCommissions accepted unsupported locale")
	}
	if _, err := client.GetProducts(context.Background(), 1001, 0); err == nil {
		t.Fatal("GetProducts accepted limit above API maximum")
	}
	if _, err := client.GetProducts(context.Background(), 1, -1); err == nil {
		t.Fatal("GetProducts accepted negative offset")
	}
	if _, err := client.GetUploadTask(context.Background(), 0); err == nil {
		t.Fatal("GetUploadTask accepted a non-positive uploadID")
	}
}

func TestUnauthorizedAndRateLimitErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		wantText   string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, wantText: "API key is valid"},
		{name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "2", wantText: "retry after 2s"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"errorText":"upstream message"}`))
			}))
			defer server.Close()
			client := testClient(server, "token")
			_, err := client.GetProducts(context.Background(), 1, 0)
			if err == nil || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("error = %v, want text %q", err, test.wantText)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != test.status {
				t.Fatalf("error = %#v, want APIError with status %d", err, test.status)
			}
		})
	}
}

func TestAPIErrorRedactsAPIKey(t *testing.T) {
	const token = "secret-in-response"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, `{"errorText":%q}`, token)
	}))
	defer server.Close()
	client := testClient(server, token)
	client.sleep = func(context.Context, time.Duration) error { return nil }

	_, err := client.GetProducts(context.Background(), 1, 0)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("API error exposed configured API key: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("API error did not explain the redacted server message: %v", err)
	}
}

func TestRetriesServerErrorsWithExponentialBackoff(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"listGoods":[]}}`))
	}))
	defer server.Close()
	client := testClient(server, "key")
	var delays []time.Duration
	client.sleep = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}

	if _, err := client.GetProducts(context.Background(), 1, 0); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 {
		t.Fatalf("request count = %d, want 3", requests.Load())
	}
	if len(delays) != 2 || delays[0] != 100*time.Millisecond || delays[1] != 200*time.Millisecond {
		t.Fatalf("retry delays = %v, want [100ms 200ms]", delays)
	}
}

func TestContextCancellationStopsRetry(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := testClient(server, "key")
	ctx, cancel := context.WithCancel(context.Background())
	client.sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}

	_, err := client.GetProducts(ctx, 1, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("request count = %d, want exactly one before cancellation", requests.Load())
	}
}

func TestGetPriceHistoryReportsUnsupportedTaskEnumeration(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := testClient(server, "key")
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	_, err := client.GetPriceHistory(context.Background(), 123, from, to)
	if !errors.Is(err, ErrUploadTaskEnumerationUnsupported) {
		t.Fatalf("error = %v, want ErrUploadTaskEnumerationUnsupported", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("GetPriceHistory made %d speculative API requests", requests.Load())
	}
}

func TestGetPriceHistoryForUploadsQueriesAndFiltersProcessedTasks(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	var detailOffsets []string
	var taskIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/history/tasks":
			id := r.URL.Query().Get("uploadID")
			taskIDs = append(taskIDs, id)
			activation := map[string]string{
				"10": from.Format(time.RFC3339),
				"11": from.Add(-time.Second).Format(time.RFC3339),
				"12": from.Add(time.Hour).Format(time.RFC3339),
				"13": to.Format(time.RFC3339),
			}[id]
			status := map[string]int{"10": 5, "11": 3, "12": 4, "13": 3}[id]
			fmt.Fprintf(w, `{"data":{"uploadID":%s,"status":%d,"uploadDate":%q,"activationDate":%q}}`,
				id, status, from.Format(time.RFC3339), activation)
		case "/api/v2/history/goods/task":
			query := r.URL.Query()
			if query.Get("uploadID") != "10" && query.Get("uploadID") != "13" {
				t.Errorf("unexpected details uploadID: %v", query)
			}
			if query.Get("limit") != "2" {
				t.Errorf("limit = %q, want 2", query.Get("limit"))
			}
			detailOffsets = append(detailOffsets, query.Get("offset"))
			uploadID := query.Get("uploadID")
			var goods string
			if query.Get("offset") == "0" && uploadID == "10" {
				goods = `[
					{"nmID":123,"sizeID":1,"techSizeName":"S","price":1500,"currencyIsoCode4217":"RUB","discount":10,"status":2},
					{"nmID":999,"price":2200,"status":2},
					{"nmID":123,"price":1600,"status":3}
				]`
			} else if query.Get("offset") == "2" && uploadID == "10" {
				goods = `[{"nmID":123,"sizeID":2,"price":1700,"discount":5,"status":2}]`
			} else {
				goods = `[{"nmID":123,"price":1800,"status":2}]`
			}
			fmt.Fprintf(w, `{"data":{"uploadID":%s,"historyGoods":%s}}`, uploadID, goods)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := testClient(server, "key")
	client.pageSize = 2

	points, err := client.GetPriceHistoryForUploads(context.Background(), 123, from, to, []int64{10, 10, 11, 12, 13})
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 3 {
		t.Fatalf("got %d price points, want 3: %#v", len(points), points)
	}
	if points[0].Price != 1500 || points[0].UploadID != 10 || !points[0].Timestamp.Equal(from) ||
		points[1].Price != 1700 || points[1].UploadID != 10 ||
		points[2].Price != 1800 || points[2].UploadID != 13 || !points[2].Timestamp.Equal(to) {
		t.Fatalf("unexpected filtered price points: %#v", points)
	}
	if got := strings.Join(taskIDs, ","); got != "10,11,12,13" {
		t.Fatalf("task lookups = %q, want unique upload IDs", got)
	}
	if got := strings.Join(detailOffsets, ","); got != "0,2,0" {
		t.Fatalf("detail offsets = %q, want 0,2 for upload 10 and 0 for upload 13", got)
	}
}

func TestGetUploadTaskBuildsQueryAndDecodesTask(t *testing.T) {
	wantID := int64(7654321)
	activation := time.Date(2026, 2, 1, 10, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/api/v2/history/tasks" {
			t.Errorf("path = %q, want /api/v2/history/tasks", got)
		}
		if got := r.URL.Query().Get("uploadID"); got != strconv.FormatInt(wantID, 10) {
			t.Errorf("uploadID = %q, want %d", got, wantID)
		}
		fmt.Fprintf(w, `{"data":{"uploadID":%d,"status":3,"uploadDate":"2026-02-01T09:00:00+03:00","activationDate":%q,"overAllGoodsNumber":2,"successGoodsNumber":2}}`,
			wantID, activation.Format(time.RFC3339))
	}))
	defer server.Close()

	task, err := testClient(server, "key").GetUploadTask(context.Background(), wantID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != 3 || task.UploadID != wantID || task.ActivationDate == nil || !task.ActivationDate.Equal(activation) {
		t.Fatalf("unexpected task metadata: %#v", task)
	}
}
