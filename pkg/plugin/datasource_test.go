package plugin

import "testing"

func TestValidateRequestPath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "relative API path", input: "/api/v1/tariffs?limit=10"},
		{name: "absolute URL", input: "https://example.com/api", wantErr: true},
		{name: "network path reference", input: "//example.com/api", wantErr: true},
		{name: "missing leading slash", input: "api/v1/tariffs", wantErr: true},
		{name: "parent segment", input: "/api/../private", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateRequestPath(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateRequestPath(%q) error = %v, wantErr %v", test.input, err, test.wantErr)
			}
		})
	}
}

func TestMarketplaceRoutesUseExpectedAPIHosts(t *testing.T) {
	want := map[string]string{
		"wb-tariffs": "https://common-api.wildberries.ru",
		"wb-prices":  "https://discounts-prices-api.wildberries.ru",
		"ozon":       "https://api-seller.ozon.ru",
	}
	for route, host := range want {
		if got := marketplaceRoutes[route].host; got != host {
			t.Errorf("route %q host = %q, want %q", route, got, host)
		}
	}
}
