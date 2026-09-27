// Package wildberries provides a small, typed client for the Wildberries Seller API.
package wildberries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	commonAPIBaseURL = "https://common-api.wildberries.ru"
	pricesAPIBaseURL = "https://discounts-prices-api.wildberries.ru"

	httpTimeout       = 30 * time.Second
	maxResponseBytes  = 5 << 20
	maxRetries        = 3
	initialRetryDelay = 100 * time.Millisecond
	maxPageSize       = 1000
)

// ErrUploadTaskEnumerationUnsupported indicates that the Seller API cannot
// provide the upload IDs needed to build complete price history for a date range.
var ErrUploadTaskEnumerationUnsupported = errors.New("Wildberries API does not support listing processed upload tasks")

// Client accesses selected Wildberries Seller API endpoints.
type Client struct {
	apiKey     string
	httpClient *http.Client
	logger     *log.Logger
	sleep      func(context.Context, time.Duration) error

	commonBaseURL string
	pricesBaseURL string
	pageSize      int
}

// NewClient creates a Wildberries Seller API client using apiKey for
// Authorization. Requests have a 30-second timeout.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: httpTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				// Do not forward the seller token to a redirect destination.
				return http.ErrUseLastResponse
			},
		},
		logger:        log.Default(),
		sleep:         sleepContext,
		commonBaseURL: commonAPIBaseURL,
		pricesBaseURL: pricesAPIBaseURL,
		pageSize:      maxPageSize,
	}
}

// APIError describes a non-success HTTP response from Wildberries.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return "Wildberries API unauthorized (HTTP 401): check that the API key is valid and has not expired"
	case http.StatusTooManyRequests:
		if e.RetryAfter > 0 {
			return fmt.Sprintf("Wildberries API rate limit exceeded (HTTP 429); retry after %s", e.RetryAfter.Round(time.Millisecond))
		}
		return "Wildberries API rate limit exceeded (HTTP 429); retry later"
	}
	if e.Message != "" {
		return fmt.Sprintf("Wildberries API returned HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("Wildberries API returned HTTP %d", e.StatusCode)
}

type apiResponseError struct {
	Error     bool   `json:"error"`
	ErrorText string `json:"errorText"`
	Message   string `json:"message"`
	Detail    string `json:"detail"`
}

func (c *Client) getJSON(ctx context.Context, baseURL, path string, query url.Values, target any) error {
	if c.apiKey == "" {
		return errors.New("Wildberries API key is empty; provide a seller API key to NewClient")
	}
	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid Wildberries API base URL: %w", err)
	}
	parsedURL := parsedBase.ResolveReference(&url.URL{Path: path})
	parsedURL.RawQuery = query.Encode()

	for attempt := 0; attempt <= maxRetries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
		if err != nil {
			return fmt.Errorf("create Wildberries API request: %w", err)
		}
		request.Header.Set("Authorization", c.apiKey)
		request.Header.Set("Accept", "application/json")

		started := time.Now()
		response, err := c.httpClient.Do(request)
		statusCode := 0
		if response != nil {
			statusCode = response.StatusCode
		}
		c.logger.Printf("wildberries request method=%s path=%s status=%d duration=%s",
			request.Method, request.URL.Path, statusCode, time.Since(started))
		if err != nil {
			return fmt.Errorf("Wildberries API request failed: %w", err)
		}

		if response.StatusCode >= 500 && response.StatusCode <= 599 && attempt < maxRetries {
			_ = response.Body.Close()
			delay := initialRetryDelay * time.Duration(1<<attempt)
			if err := c.sleep(ctx, delay); err != nil {
				return fmt.Errorf("wait to retry Wildberries API request: %w", err)
			}
			continue
		}
		body, readErr := readLimitedAndClose(response.Body)
		if readErr != nil {
			return fmt.Errorf("read Wildberries API response: %w", readErr)
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			apiErr := newAPIError(request.Method, request.URL.Path, response, body)
			apiErr.Message = strings.ReplaceAll(apiErr.Message, c.apiKey, "[redacted]")
			return apiErr
		}

		if err := json.Unmarshal(body, target); err != nil {
			return fmt.Errorf("decode Wildberries API response: %w", err)
		}
		return nil
	}
	return errors.New("Wildberries API request exhausted retries")
}

func readLimitedAndClose(body io.ReadCloser) ([]byte, error) {
	defer body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(responseBody) > maxResponseBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}
	return responseBody, nil
}

func newAPIError(method, path string, response *http.Response, body []byte) *APIError {
	apiErr := &APIError{
		Method:     method,
		Path:       path,
		StatusCode: response.StatusCode,
		RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()),
	}
	var payload apiResponseError
	if json.Unmarshal(body, &payload) == nil {
		apiErr.Message = firstNonEmpty(payload.ErrorText, payload.Message, payload.Detail)
	}
	return apiErr
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return retryAt.Sub(now)
	}
	return 0
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func checkAPIEnvelope(apiErr apiResponseError) error {
	if !apiErr.Error {
		return nil
	}
	message := firstNonEmpty(apiErr.ErrorText, apiErr.Message, apiErr.Detail)
	if message == "" {
		message = "unspecified API error"
	}
	return fmt.Errorf("Wildberries API error: %s", message)
}
