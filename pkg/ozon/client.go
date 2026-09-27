// Package ozon provides a typed client for selected Ozon Seller API endpoints.
package ozon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	apiBaseURL      = "https://api-seller.ozon.ru"
	httpTimeout     = 30 * time.Second
	maxResponseSize = 5 << 20
	maxPageSize     = 1000
)

// Client accesses selected Ozon Seller API endpoints.
type Client struct {
	clientID   string
	apiKey     string
	httpClient *http.Client
	logger     *log.Logger
	baseURL    string
}

// NewClient creates an Ozon Seller API client. Requests have a 30-second timeout.
func NewClient(clientID, apiKey string) *Client {
	return &Client{
		clientID: clientID,
		apiKey:   apiKey,
		httpClient: &http.Client{
			Timeout: httpTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		logger:  log.Default(),
		baseURL: apiBaseURL,
	}
}

// APIError describes an HTTP or application-level error returned by Ozon.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Code       string
	Message    string
	RequestID  string
}

func (e *APIError) Error() string {
	message := fmt.Sprintf("Ozon API returned HTTP %d", e.StatusCode)
	if e.Code != "" {
		message += " (code " + e.Code + ")"
	}
	if e.Message != "" {
		message += ": " + e.Message
	}
	if e.RequestID != "" {
		message += " (request ID " + e.RequestID + ")"
	}
	return message
}

type errorEnvelope struct {
	Code      json.RawMessage `json:"code"`
	Message   string          `json:"message"`
	RequestID string          `json:"request_id"`
}

func (c *Client) postJSON(ctx context.Context, path string, body any, target any) error {
	if c.clientID == "" {
		return errors.New("Ozon Client-Id is empty; provide a client ID to NewClient")
	}
	if c.apiKey == "" {
		return errors.New("Ozon Api-Key is empty; provide an API key to NewClient")
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode Ozon API request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create Ozon API request: %w", err)
	}
	request.Header.Set("Client-Id", c.clientID)
	request.Header.Set("Api-Key", c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	started := time.Now()
	response, requestErr := c.httpClient.Do(request)
	statusCode := 0
	requestID := ""
	if response != nil {
		statusCode = response.StatusCode
		requestID = firstNonEmpty(
			response.Header.Get("X-Ozon-Request-Id"),
			response.Header.Get("X-Request-Id"),
			response.Header.Get("X-O3-Trace-Id"),
		)
	}
	if requestErr != nil {
		c.logger.Printf("ozon request method=%s path=%s status=%d request_id=%q duration=%s",
			request.Method, request.URL.Path, statusCode, requestID, time.Since(started))
		return fmt.Errorf("Ozon API request failed: %w", requestErr)
	}
	responseBody, err := readLimitedAndClose(response.Body)
	if err != nil {
		c.logger.Printf("ozon request method=%s path=%s status=%d request_id=%q duration=%s",
			request.Method, request.URL.Path, statusCode, requestID, time.Since(started))
		return fmt.Errorf("read Ozon API response: %w", err)
	}

	var envelope errorEnvelope
	_ = json.Unmarshal(responseBody, &envelope)
	if requestID == "" {
		requestID = strings.TrimSpace(envelope.RequestID)
	}
	c.logger.Printf("ozon request method=%s path=%s status=%d request_id=%q duration=%s",
		request.Method, request.URL.Path, statusCode, requestID, time.Since(started))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return c.newAPIError(request, response.StatusCode, response.Header, responseBody, envelope, requestID)
	}
	if code := parseErrorCode(envelope.Code); code != "" && code != "0" {
		return c.newAPIError(request, response.StatusCode, response.Header, responseBody, envelope, requestID)
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return fmt.Errorf("decode Ozon API response: %w", err)
	}
	return nil
}

func (c *Client) newAPIError(request *http.Request, statusCode int, headers http.Header, body []byte, envelope errorEnvelope, requestID string) *APIError {
	if requestID == "" {
		requestID = firstNonEmpty(headers.Get("X-Ozon-Request-Id"), headers.Get("X-Request-Id"), headers.Get("X-O3-Trace-Id"))
	}
	message := strings.TrimSpace(envelope.Message)
	message = strings.ReplaceAll(message, c.apiKey, "[redacted]")
	message = strings.ReplaceAll(message, c.clientID, "[redacted]")
	if message == "" && len(body) == 0 {
		message = http.StatusText(statusCode)
	}
	return &APIError{
		Method:     request.Method,
		Path:       request.URL.Path,
		StatusCode: statusCode,
		Code:       parseErrorCode(envelope.Code),
		Message:    message,
		RequestID:  requestID,
	}
}

func readLimitedAndClose(body io.ReadCloser) ([]byte, error) {
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxResponseSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseSize {
		return nil, fmt.Errorf("response exceeds %d bytes", maxResponseSize)
	}
	return data, nil
}

func parseErrorCode(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(string(raw))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
