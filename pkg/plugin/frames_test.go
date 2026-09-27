package plugin

import (
	"fmt"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func TestNewCommissionsFrame(t *testing.T) {
	instant := time.Date(2026, time.September, 27, 15, 4, 5, 6, time.FixedZone("UTC+3", 3*60*60))
	frame := NewCommissionsFrame([]CommissionRow{{
		Time:          instant,
		Category:      "Electronics",
		CommissionPct: 12.5,
		LogisticsCost: 34.25,
		StorageCost:   5.75,
	}})

	assertFrameSchema(t, frame, "commissions", []string{
		"time", "category", "commission_pct", "logistics_cost", "storage_cost",
	}, []data.FieldType{
		data.FieldTypeTime, data.FieldTypeString, data.FieldTypeFloat64,
		data.FieldTypeFloat64, data.FieldTypeFloat64,
	})
	assertUTCValue(t, frame.Fields[0], instant)
	assertFieldConfig(t, frame.Fields[0], "Time", "")
	assertFieldConfig(t, frame.Fields[2], "Commission (%)", "percent")
	assertFieldConfig(t, frame.Fields[3], "Logistics Cost", "currencyRUB")
	assertFieldConfig(t, frame.Fields[4], "Storage Cost", "currencyRUB")
	if got := []interface{}{
		frame.Fields[1].At(0), frame.Fields[2].At(0), frame.Fields[3].At(0), frame.Fields[4].At(0),
	}; fmt.Sprint(got) != "[Electronics 12.5 34.25 5.75]" {
		t.Fatalf("commission values = %v, want [Electronics 12.5 34.25 5.75]", got)
	}
}

func TestNewProfitabilityFrame(t *testing.T) {
	instant := time.Date(2026, time.September, 27, 15, 4, 5, 6, time.FixedZone("UTC+3", 3*60*60))
	frame := NewProfitabilityFrame([]ProfitabilityRow{{
		Time:         instant,
		ProductName:  "Desk lamp",
		Price:        999.5,
		NetMarginRUB: -25.25,
		NetMarginPct: -2.53,
		IsLoss:       true,
	}})

	assertFrameSchema(t, frame, "profitability", []string{
		"time", "product_name", "price", "net_margin_rub", "net_margin_pct", "is_loss",
	}, []data.FieldType{
		data.FieldTypeTime, data.FieldTypeString, data.FieldTypeFloat64,
		data.FieldTypeFloat64, data.FieldTypeFloat64, data.FieldTypeBool,
	})
	assertUTCValue(t, frame.Fields[0], instant)
	assertFieldConfig(t, frame.Fields[1], "Product Name", "")
	assertFieldConfig(t, frame.Fields[2], "Price", "currencyRUB")
	assertFieldConfig(t, frame.Fields[3], "Net Margin (RUB)", "currencyRUB")
	assertFieldConfig(t, frame.Fields[4], "Net Margin (%)", "percent")
	assertFieldConfig(t, frame.Fields[5], "Is Loss", "")
	if got := []interface{}{
		frame.Fields[1].At(0), frame.Fields[2].At(0), frame.Fields[3].At(0),
		frame.Fields[4].At(0), frame.Fields[5].At(0),
	}; fmt.Sprint(got) != "[Desk lamp 999.5 -25.25 -2.53 true]" {
		t.Fatalf("profitability values = %v, want [Desk lamp 999.5 -25.25 -2.53 true]", got)
	}
}

func TestNewPriceHistoryFrame(t *testing.T) {
	instant := time.Date(2026, time.September, 27, 15, 4, 5, 6, time.FixedZone("UTC+3", 3*60*60))
	frame := NewPriceHistoryFrame([]PriceHistoryRow{{
		Time:      instant,
		ProductID: "sku-42",
		Price:     123.45,
	}})

	assertFrameSchema(t, frame, "price_history", []string{"time", "product_id", "price"}, []data.FieldType{
		data.FieldTypeTime, data.FieldTypeString, data.FieldTypeFloat64,
	})
	assertUTCValue(t, frame.Fields[0], instant)
	assertFieldConfig(t, frame.Fields[1], "Product ID", "")
	assertFieldConfig(t, frame.Fields[2], "Price", "currencyRUB")
	if frame.Fields[1].At(0) != "sku-42" || frame.Fields[2].At(0) != 123.45 {
		t.Fatalf("price history values = %v/%v, want sku-42/123.45", frame.Fields[1].At(0), frame.Fields[2].At(0))
	}
}

func TestMarketplaceFramesAcceptEmptyRows(t *testing.T) {
	tests := []struct {
		name  string
		frame *data.Frame
	}{
		{name: "commissions", frame: NewCommissionsFrame(nil)},
		{name: "profitability", frame: NewProfitabilityFrame(nil)},
		{name: "price history", frame: NewPriceHistoryFrame(nil)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.frame.Rows() != 0 {
				t.Fatalf("frame has %d rows, want zero", test.frame.Rows())
			}
			for _, field := range test.frame.Fields {
				if got := field.Len(); got != 0 {
					t.Errorf("field %q has %d values, want zero", field.Name, got)
				}
			}
		})
	}
}

func assertFrameSchema(t *testing.T, frame *data.Frame, wantName string, names []string, types []data.FieldType) {
	t.Helper()
	if frame.Name != wantName {
		t.Fatalf("frame name = %q, want %q", frame.Name, wantName)
	}
	if len(frame.Fields) != len(names) {
		t.Fatalf("frame has %d fields, want %d", len(frame.Fields), len(names))
	}
	for index, field := range frame.Fields {
		if field.Name != names[index] {
			t.Errorf("field %d name = %q, want %q", index, field.Name, names[index])
		}
		if field.Type() != types[index] {
			t.Errorf("field %q type = %v, want %v", field.Name, field.Type(), types[index])
		}
		if field.Len() != frame.Rows() {
			t.Errorf("field %q length = %d, want frame row count %d", field.Name, field.Len(), frame.Rows())
		}
	}
}

func assertUTCValue(t *testing.T, field *data.Field, want time.Time) {
	t.Helper()
	got, ok := field.At(0).(time.Time)
	if !ok {
		t.Fatalf("time field value has type %T, want time.Time", field.At(0))
	}
	if !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("time value = %s (%s), want %s in UTC", got, got.Location(), want.UTC())
	}
}

func assertFieldConfig(t *testing.T, field *data.Field, displayName, unit string) {
	t.Helper()
	if field.Config == nil {
		t.Fatalf("field %q has no config", field.Name)
	}
	if field.Config.DisplayNameFromDS != displayName {
		t.Errorf("field %q datasource display name = %q, want %q", field.Name, field.Config.DisplayNameFromDS, displayName)
	}
	if field.Config.Unit != unit {
		t.Errorf("field %q unit = %q, want %q", field.Name, field.Config.Unit, unit)
	}
}

func ExampleNewCommissionsFrame() {
	frame := NewCommissionsFrame([]CommissionRow{{
		Time:          time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC),
		Category:      "Electronics",
		CommissionPct: 12.5,
		LogisticsCost: 34,
		StorageCost:   5,
	}})
	fmt.Println(frame.Name, frame.Rows())
	// Output: commissions 1
}

func ExampleNewProfitabilityFrame() {
	frame := NewProfitabilityFrame([]ProfitabilityRow{{
		Time:         time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC),
		ProductName:  "Desk lamp",
		Price:        999,
		NetMarginRUB: 125,
		NetMarginPct: 12.5,
		IsLoss:       false,
	}})
	fmt.Println(frame.Name, frame.Rows())
	// Output: profitability 1
}

func ExampleNewPriceHistoryFrame() {
	frame := NewPriceHistoryFrame([]PriceHistoryRow{{
		Time:      time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC),
		ProductID: "sku-42",
		Price:     123.45,
	}})
	fmt.Println(frame.Name, frame.Rows())
	// Output: price_history 1
}
