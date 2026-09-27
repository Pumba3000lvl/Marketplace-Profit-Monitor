package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const maxConnectionResponseBytes = 64 << 10

type connectionTestRequest struct {
	Marketplaces []string `json:"marketplaces"`
}

type connectionTestResult struct {
	Marketplace string `json:"marketplace"`
	OK          bool   `json:"ok"`
	Message     string `json:"message"`
}

type connectionTestResponse struct {
	Results []connectionTestResult `json:"results"`
}

func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if req.Path != "test-connection" && req.Path != "/test-connection" {
		return sendResourceJSON(sender, http.StatusNotFound, map[string]string{"error": "Неизвестный ресурс источника данных."})
	}
	if req.Method != http.MethodPost {
		return sendResourceJSON(sender, http.StatusMethodNotAllowed, map[string]string{"error": "Допустимый метод запроса: POST."})
	}
	if len(req.Body) == 0 || len(req.Body) > 4096 {
		return sendResourceJSON(sender, http.StatusBadRequest, map[string]string{"error": "Некорректный запрос проверки подключения."})
	}

	var testRequest connectionTestRequest
	if err := json.Unmarshal(req.Body, &testRequest); err != nil || len(testRequest.Marketplaces) == 0 {
		return sendResourceJSON(sender, http.StatusBadRequest, map[string]string{"error": "Выберите хотя бы один настроенный маркетплейс."})
	}
	seen := make(map[string]struct{}, len(testRequest.Marketplaces))
	for _, name := range testRequest.Marketplaces {
		if name != "wildberries" && name != "ozon" {
			return sendResourceJSON(sender, http.StatusBadRequest, map[string]string{"error": "Неизвестный маркетплейс."})
		}
		if _, exists := seen[name]; exists {
			return sendResourceJSON(sender, http.StatusBadRequest, map[string]string{"error": "Маркетплейс указан несколько раз."})
		}
		seen[name] = struct{}{}
		if err := d.validateCredentials(name); err != nil {
			return sendResourceJSON(sender, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
	}

	return sendResourceJSON(sender, http.StatusOK, connectionTestResponse{
		Results: d.testConnections(ctx, testRequest.Marketplaces),
	})
}

func sendResourceJSON(sender backend.CallResourceResponseSender, status int, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode datasource resource response: %w", err)
	}
	return sender.Send(&backend.CallResourceResponse{
		Status: status,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
		Body: body,
	})
}

func (d *Datasource) configuredMarketplaces() ([]string, error) {
	hasWildberries := strings.TrimSpace(d.credentials.wildberriesToken) != ""
	hasOzonClientID := strings.TrimSpace(d.credentials.ozonClientID) != ""
	hasOzonAPIKey := strings.TrimSpace(d.credentials.ozonAPIKey) != ""

	if hasOzonClientID != hasOzonAPIKey {
		return nil, errors.New("Укажите Client-Id и API-ключ Ozon.")
	}
	if hasOzonClientID && !isNumericID(strings.TrimSpace(d.credentials.ozonClientID)) {
		return nil, errors.New("Client-Id Ozon должен содержать только цифры.")
	}
	var configured []string
	if hasWildberries {
		configured = append(configured, "wildberries")
	}
	if hasOzonClientID {
		configured = append(configured, "ozon")
	}
	if len(configured) == 0 {
		return nil, errors.New("Укажите API-ключ Wildberries или оба учётных параметра Ozon.")
	}
	return configured, nil
}

func (d *Datasource) testConnections(ctx context.Context, marketplaces []string) []connectionTestResult {
	results := make([]connectionTestResult, 0, len(marketplaces))
	for _, marketplace := range marketplaces {
		message := d.testConnection(ctx, marketplace)
		results = append(results, connectionTestResult{
			Marketplace: marketplace,
			OK:          message == "",
			Message:     message,
		})
	}
	return results
}

func (d *Datasource) testConnection(ctx context.Context, marketplace string) string {
	var request *http.Request
	var err error
	switch marketplace {
	case "wildberries":
		request, err = http.NewRequestWithContext(ctx, http.MethodGet,
			marketplaceRoutes["wb-tariffs"].host+"/api/v1/tariffs/commission?locale=ru", nil)
		if err == nil {
			request.Header.Set("Authorization", d.credentials.wildberriesToken)
		}
	case "ozon":
		payload := []byte(`{"filter":{"offer_id":[],"product_id":[],"visibility":"ALL"},"last_id":"","limit":1}`)
		request, err = http.NewRequestWithContext(ctx, http.MethodPost,
			marketplaceRoutes["ozon"].host+"/v3/product/list", bytes.NewReader(payload))
		if err == nil {
			request.Header.Set("Client-Id", d.credentials.ozonClientID)
			request.Header.Set("Api-Key", d.credentials.ozonAPIKey)
			request.Header.Set("Content-Type", "application/json")
		}
	default:
		return "Неизвестный маркетплейс."
	}
	if err != nil {
		return "Не удалось сформировать запрос к API маркетплейса."
	}

	response, err := d.client.Do(request)
	if err != nil {
		return "Не удалось подключиться к API маркетплейса."
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxConnectionResponseBytes+1))
	if err != nil || len(body) > maxConnectionResponseBytes {
		return "Не удалось прочитать ответ API маркетплейса."
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "Не удалось пройти аутентификацию. Проверьте учётные данные и доступ к API."
		case http.StatusTooManyRequests:
			return "Превышен лимит запросов к API маркетплейса. Повторите попытку позже."
		default:
			return fmt.Sprintf("API маркетплейса вернул код HTTP %d.", response.StatusCode)
		}
	}
	if err := checkConnectionResponse(marketplace, body); err != nil {
		return err.Error()
	}
	return ""
}

func checkConnectionResponse(marketplace string, body []byte) error {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return errors.New("API маркетплейса вернул некорректный ответ.")
	}
	if marketplace == "wildberries" {
		var hasError bool
		if err := json.Unmarshal(envelope["error"], &hasError); err == nil && hasError {
			return errors.New("Wildberries отклонил запрос. Проверьте API-ключ и права доступа.")
		}
		return nil
	}
	if rawCode := strings.TrimSpace(string(envelope["code"])); rawCode != "" && rawCode != "null" {
		code := strings.Trim(strings.TrimSpace(rawCode), `"`)
		if code != "" && code != "0" {
			return errors.New("Ozon отклонил запрос. Проверьте учётные данные и доступ к API.")
		}
	}
	return nil
}

func (d *Datasource) validateCredentials(marketplace string) error {
	switch marketplace {
	case "wildberries", "wb-tariffs", "wb-prices":
		if strings.TrimSpace(d.credentials.wildberriesToken) == "" {
			return errors.New("Укажите API-ключ Wildberries.")
		}
	case "ozon":
		clientID := strings.TrimSpace(d.credentials.ozonClientID)
		if clientID == "" || strings.TrimSpace(d.credentials.ozonAPIKey) == "" {
			return errors.New("Укажите Client-Id и API-ключ Ozon.")
		}
		if !isNumericID(clientID) {
			return errors.New("Client-Id Ozon должен содержать только цифры.")
		}
	default:
		return errors.New("Неизвестный маркетплейс.")
	}
	return nil
}

func isNumericID(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func displayMarketplaceName(name string) string {
	switch name {
	case "wildberries":
		return "Wildberries"
	case "ozon":
		return "Ozon"
	default:
		return name
	}
}
