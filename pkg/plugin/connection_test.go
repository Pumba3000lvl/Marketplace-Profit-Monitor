package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestNewDatasourceLoadsPublicAndSecureSettings(t *testing.T) {
	instance, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{
		JSONData: json.RawMessage(`{"ozonClientId":"12345","telegramChatId":"-100123"}`),
		DecryptedSecureJSONData: map[string]string{
			"wildberriesToken": "wb-secret",
			"ozonApiKey":       "ozon-secret",
			"telegramBotToken": "123:telegram-secret",
		},
	})
	if err != nil {
		t.Fatalf("NewDatasource() error = %v", err)
	}
	datasource := instance.(*Datasource)
	if datasource.credentials.wildberriesToken != "wb-secret" ||
		datasource.credentials.ozonClientID != "12345" ||
		datasource.credentials.ozonAPIKey != "ozon-secret" ||
		datasource.credentials.telegramBotToken != "123:telegram-secret" ||
		datasource.jsonData.TelegramChatID != "-100123" {
		t.Fatalf("datasource settings were not loaded correctly: %+v", datasource)
	}
}

func TestValidateCredentialsAcceptsQueryRoutes(t *testing.T) {
	datasource := &Datasource{credentials: credentials{
		wildberriesToken: "wb-secret",
		ozonClientID:     "12345",
		ozonAPIKey:       "ozon-secret",
	}}
	for _, route := range []string{"wb-tariffs", "wb-prices"} {
		if err := datasource.validateCredentials(route); err != nil {
			t.Errorf("validateCredentials(%q) error = %v", route, err)
		}
	}
	if err := datasource.validateCredentials("ozon"); err != nil {
		t.Errorf("validateCredentials(%q) error = %v", "ozon", err)
	}
}

func TestCallResourceTestsConfiguredMarketplacesServerSide(t *testing.T) {
	datasource := &Datasource{
		credentials: credentials{
			wildberriesToken: "wb-secret",
			ozonClientID:     "12345",
			ozonAPIKey:       "ozon-secret",
		},
	}
	datasource.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Host == "common-api.wildberries.ru":
			if request.Method != http.MethodGet || request.URL.Path != "/api/v1/tariffs/commission" ||
				request.Header.Get("Authorization") != "wb-secret" {
				t.Errorf("Wildberries probe = %s %s headers=%v", request.Method, request.URL, request.Header)
			}
			return jsonResponse(http.StatusOK, `{"report":[]}`), nil
		case request.URL.Host == "api-seller.ozon.ru":
			if request.Method != http.MethodPost || request.URL.Path != "/v3/product/list" ||
				request.Header.Get("Client-Id") != "12345" || request.Header.Get("Api-Key") != "ozon-secret" {
				t.Errorf("Ozon probe = %s %s headers=%v", request.Method, request.URL, request.Header)
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Errorf("read Ozon probe body: %v", err)
			}
			if !strings.Contains(string(body), `"limit":1`) {
				t.Errorf("Ozon probe body = %s, want limit 1", body)
			}
			return jsonResponse(http.StatusOK, `{"result":{"items":[],"last_id":"","total":0}}`), nil
		default:
			t.Errorf("unexpected upstream host: %s", request.URL.Host)
			return jsonResponse(http.StatusBadGateway, `{}`), nil
		}
	})}

	response := &capturedResourceResponse{}
	err := datasource.CallResource(context.Background(), &backend.CallResourceRequest{
		Path:   "test-connection",
		Method: http.MethodPost,
		Body:   []byte(`{"marketplaces":["wildberries","ozon"]}`),
	}, response)
	if err != nil {
		t.Fatalf("CallResource() error = %v", err)
	}
	if response.status != http.StatusOK {
		t.Fatalf("CallResource() status = %d, want %d: %s", response.status, http.StatusOK, response.body)
	}
	var result connectionTestResponse
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatalf("decode CallResource() response: %v", err)
	}
	if len(result.Results) != 2 || !result.Results[0].OK || !result.Results[1].OK {
		t.Fatalf("connection results = %+v, want two successes", result.Results)
	}
}

func TestCallResourceSanitizesMarketplaceErrors(t *testing.T) {
	const secret = "do-not-return-this-secret"
	datasource := &Datasource{
		credentials: credentials{wildberriesToken: secret},
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("Authorization") != secret {
				t.Errorf("authorization = %q, want configured key", request.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusUnauthorized, `{"error":"`+secret+`"}`), nil
		})},
	}
	response := &capturedResourceResponse{}
	err := datasource.CallResource(context.Background(), &backend.CallResourceRequest{
		Path:   "test-connection",
		Method: http.MethodPost,
		Body:   []byte(`{"marketplaces":["wildberries"]}`),
	}, response)
	if err != nil {
		t.Fatalf("CallResource() error = %v", err)
	}
	if strings.Contains(string(response.body), secret) {
		t.Fatalf("response exposed the API key: %s", response.body)
	}
	var result connectionTestResponse
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatalf("decode CallResource() response: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].OK ||
		result.Results[0].Message != "Не удалось пройти аутентификацию. Проверьте учётные данные и доступ к API." {
		t.Fatalf("connection result = %+v, want a sanitized authentication failure", result.Results)
	}
}

func TestCallResourceRejectsUnknownAndUnconfiguredTargets(t *testing.T) {
	testCases := []struct {
		name string
		path string
		body string
		want int
	}{
		{name: "unknown path", path: "other", body: `{"marketplaces":["wildberries"]}`, want: http.StatusNotFound},
		{name: "unconfigured", path: "test-connection", body: `{"marketplaces":["ozon"]}`, want: http.StatusBadRequest},
		{name: "unknown marketplace", path: "test-connection", body: `{"marketplaces":["unknown"]}`, want: http.StatusBadRequest},
		{name: "empty targets", path: "test-connection", body: `{"marketplaces":[]}`, want: http.StatusBadRequest},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			response := &capturedResourceResponse{}
			err := (&Datasource{}).CallResource(context.Background(), &backend.CallResourceRequest{
				Path: testCase.path, Method: http.MethodPost, Body: []byte(testCase.body),
			}, response)
			if err != nil {
				t.Fatalf("CallResource() error = %v", err)
			}
			if response.status != testCase.want {
				t.Fatalf("CallResource() status = %d, want %d", response.status, testCase.want)
			}
		})
	}
}

func TestQueryDataUsesDecryptedSettingsWithoutExposingCredentials(t *testing.T) {
	const secret = "query-only-wb-secret"
	instance, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{
		DecryptedSecureJSONData: map[string]string{"wildberriesToken": secret},
	})
	if err != nil {
		t.Fatalf("NewDatasource() error = %v", err)
	}
	datasource := instance.(*Datasource)
	datasource.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != secret {
			t.Errorf("authorization header = %q, want configured secure setting", request.Header.Get("Authorization"))
		}
		return jsonResponse(http.StatusOK, `{"token":"`+secret+`"}`), nil
	})}

	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID: "A",
		JSON:  json.RawMessage(`{"marketplace":"wb-tariffs","path":"/api/v1/tariffs/box","method":"GET"}`),
	}}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	result := response.Responses["A"]
	if result.Error != nil || result.Status != backend.StatusOK {
		t.Fatalf("QueryData() response = %+v, want successful response", result)
	}
	body := result.Frames[0].Fields[2].At(0).(string)
	if strings.Contains(body, secret) || strings.Contains(fmt.Sprint(result), secret) {
		t.Fatalf("query response exposed secure setting %q: %s", secret, body)
	}
	if !strings.Contains(body, "[redacted]") {
		t.Fatalf("response body = %s, want matching credential redacted", body)
	}
}

func TestQueryDataMapsRawRouteRateLimit(t *testing.T) {
	datasource := &Datasource{
		credentials: credentials{wildberriesToken: "configured"},
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusTooManyRequests, `{"error":"retry later"}`), nil
		})},
	}
	response, err := datasource.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID: "A",
		JSON:  json.RawMessage(`{"marketplace":"wb-tariffs","path":"/api/v1/tariffs/box","method":"GET"}`),
	}}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	if result := response.Responses["A"]; result.Status != backend.StatusTooManyRequests || result.Error == nil {
		t.Fatalf("rate-limit response = %+v, want status 429 and error", result)
	}
}

func TestQueryDataHonorsRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	datasource := &Datasource{
		credentials: credentials{wildberriesToken: "configured"},
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return nil, request.Context().Err()
		})},
	}
	response, err := datasource.QueryData(ctx, &backend.QueryDataRequest{Queries: []backend.DataQuery{{
		RefID: "A",
		JSON:  json.RawMessage(`{"marketplace":"wb-tariffs","path":"/api/v1/tariffs/box","method":"GET"}`),
	}}})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	if result := response.Responses["A"]; result.Status != backend.StatusInternal || result.Error == nil {
		t.Fatalf("cancelled query response = %+v, want per-query internal error", result)
	}
}

func TestCheckConnectionResponseDetectsMarketplaceApplicationErrors(t *testing.T) {
	tests := []struct {
		marketplace string
		body        string
		wantError   bool
	}{
		{marketplace: "wildberries", body: `{"error":true,"errorText":"secret message"}`, wantError: true},
		{marketplace: "wildberries", body: `{"error":false,"report":[]}`},
		{marketplace: "ozon", body: `{"code":"NOT_ALLOWED"}`, wantError: true},
		{marketplace: "ozon", body: `{"code":0,"result":{}}`},
		{marketplace: "ozon", body: `{"result":{}}`},
		{marketplace: "ozon", body: `not-json`, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.marketplace+"/"+test.body, func(t *testing.T) {
			err := checkConnectionResponse(test.marketplace, []byte(test.body))
			if (err != nil) != test.wantError {
				t.Fatalf("checkConnectionResponse() error = %v, wantError %t", err, test.wantError)
			}
			if err != nil && strings.Contains(err.Error(), "secret message") {
				t.Fatalf("application error exposed response content: %v", err)
			}
		})
	}
}

type capturedResourceResponse struct {
	status int
	body   []byte
}

func (r *capturedResourceResponse) Send(response *backend.CallResourceResponse) error {
	r.status = response.Status
	r.body = append([]byte(nil), response.Body...)
	return nil
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}
