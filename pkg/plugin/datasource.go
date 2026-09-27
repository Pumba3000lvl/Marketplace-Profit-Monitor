package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

const maxResponseBytes = 5 << 20
const maxRequestBytes = 1 << 20

type marketplaceRoute struct {
	host string
}

var marketplaceRoutes = map[string]marketplaceRoute{
	"wb-tariffs": {host: "https://common-api.wildberries.ru"},
	"wb-prices":  {host: "https://discounts-prices-api.wildberries.ru"},
	"ozon":       {host: "https://api-seller.ozon.ru"},
}

var (
	_ backend.QueryDataHandler   = (*Datasource)(nil)
	_ backend.CheckHealthHandler = (*Datasource)(nil)
)

type credentials struct {
	wildberriesToken string
	ozonClientID     string
	ozonAPIKey       string
}

type Datasource struct {
	client      *http.Client
	credentials credentials
}

type queryModel struct {
	Marketplace string `json:"marketplace"`
	Path        string `json:"path"`
	Method      string `json:"method"`
	Body        string `json:"body"`
}

func NewDatasource(_ context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	return &Datasource{
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		credentials: credentials{
			wildberriesToken: settings.DecryptedSecureJSONData["wildberriesToken"],
			ozonClientID:     settings.DecryptedSecureJSONData["ozonClientId"],
			ozonAPIKey:       settings.DecryptedSecureJSONData["ozonApiKey"],
		},
	}, nil
}

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	for _, query := range req.Queries {
		response.Responses[query.RefID] = d.query(ctx, query)
	}
	return response, nil
}

func (d *Datasource) query(ctx context.Context, query backend.DataQuery) backend.DataResponse {
	var model queryModel
	if err := json.Unmarshal(query.JSON, &model); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, "invalid query JSON")
	}

	route, ok := marketplaceRoutes[model.Marketplace]
	if !ok {
		return backend.ErrDataResponse(backend.StatusBadRequest, "unknown marketplace route")
	}
	requestPath, err := validateRequestPath(model.Path)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}
	method := strings.ToUpper(model.Method)
	if method != http.MethodGet && method != http.MethodPost {
		return backend.ErrDataResponse(backend.StatusBadRequest, "method must be GET or POST")
	}
	if len(model.Body) > maxRequestBytes {
		return backend.ErrDataResponse(backend.StatusBadRequest, "request body exceeds 1 MiB")
	}
	if err := d.validateCredentials(model.Marketplace); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}

	targetURL := route.host + requestPath
	var body io.Reader
	if method == http.MethodPost && model.Body != "" {
		body = bytes.NewBufferString(model.Body)
	}
	request, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, "could not create marketplace request")
	}
	d.applyCredentials(request, model.Marketplace)
	if method == http.MethodPost && model.Body != "" {
		request.Header.Set("Content-Type", "application/json")
	}

	upstreamResponse, err := d.client.Do(request)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadGateway, fmt.Sprintf("marketplace request failed: %v", err))
	}
	defer upstreamResponse.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(upstreamResponse.Body, maxResponseBytes+1))
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadGateway, "could not read marketplace response")
	}
	if len(responseBody) > maxResponseBytes {
		return backend.ErrDataResponse(backend.StatusBadGateway, "marketplace response exceeds 5 MiB")
	}
	if upstreamResponse.StatusCode < http.StatusOK || upstreamResponse.StatusCode >= http.StatusMultipleChoices {
		return backend.ErrDataResponse(
			backend.StatusBadGateway,
			fmt.Sprintf("marketplace API returned HTTP %d", upstreamResponse.StatusCode),
		)
	}

	frame := data.NewFrame(
		"marketplace-response",
		data.NewField("marketplace", nil, []string{model.Marketplace}),
		data.NewField("status", nil, []int64{int64(upstreamResponse.StatusCode)}),
		data.NewField("response", nil, []string{string(responseBody)}),
	)
	frame.RefID = query.RefID
	return backend.DataResponse{Frames: data.Frames{frame}}
}

func validateRequestPath(value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return "", fmt.Errorf("API path must be an absolute path beginning with one slash")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
		return "", fmt.Errorf("API path must be relative to the selected marketplace host")
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == ".." {
			return "", fmt.Errorf("API path cannot contain parent-directory segments")
		}
	}
	return parsed.String(), nil
}

func (d *Datasource) validateCredentials(route string) error {
	switch route {
	case "wb-tariffs", "wb-prices":
		if d.credentials.wildberriesToken == "" {
			return fmt.Errorf("configure a Wildberries API token")
		}
	case "ozon":
		if d.credentials.ozonClientID == "" || d.credentials.ozonAPIKey == "" {
			return fmt.Errorf("configure both Ozon Client-Id and API key")
		}
	}
	return nil
}

func (d *Datasource) applyCredentials(request *http.Request, route string) {
	switch route {
	case "wb-tariffs", "wb-prices":
		request.Header.Set("Authorization", d.credentials.wildberriesToken)
	case "ozon":
		request.Header.Set("Client-Id", d.credentials.ozonClientID)
		request.Header.Set("Api-Key", d.credentials.ozonAPIKey)
	}
}

func (d *Datasource) CheckHealth(_ context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if d.credentials.wildberriesToken == "" && (d.credentials.ozonClientID == "" || d.credentials.ozonAPIKey == "") {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Configure a Wildberries token or both Ozon credentials",
		}, nil
	}
	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Marketplace credentials are configured",
	}, nil
}
