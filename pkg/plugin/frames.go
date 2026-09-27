package plugin

import (
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// CommissionRow contains one category's commission and fulfillment costs.
// CommissionPct is expressed as a percentage from 0 to 100.
type CommissionRow struct {
	Time          time.Time
	Category      string
	CommissionPct float64
	LogisticsCost float64
	StorageCost   float64
}

// ProfitabilityRow contains one product's price and net margin.
// NetMarginPct is expressed as a percentage from 0 to 100.
type ProfitabilityRow struct {
	Time         time.Time
	ProductName  string
	Price        float64
	NetMarginRUB float64
	NetMarginPct float64
	IsLoss       bool
}

// PriceHistoryRow contains one product's price at a point in time.
type PriceHistoryRow struct {
	Time      time.Time
	ProductID string
	Price     float64
}

// NewCommissionsFrame converts category commission rows into a typed Grafana
// frame. Monetary costs are expressed in RUB and percentages in percent.
func NewCommissionsFrame(rows []CommissionRow) *data.Frame {
	times := make([]time.Time, len(rows))
	categories := make([]string, len(rows))
	commissionPcts := make([]float64, len(rows))
	logisticsCosts := make([]float64, len(rows))
	storageCosts := make([]float64, len(rows))
	for index, row := range rows {
		times[index] = row.Time.UTC()
		categories[index] = row.Category
		commissionPcts[index] = row.CommissionPct
		logisticsCosts[index] = row.LogisticsCost
		storageCosts[index] = row.StorageCost
	}

	return newTypedFrame("commissions",
		newTypedField("time", "Time", "", data.FieldTypeTime, times),
		newTypedField("category", "Category", "", data.FieldTypeString, categories),
		newTypedField("commission_pct", "Commission (%)", "percent", data.FieldTypeFloat64, commissionPcts),
		newTypedField("logistics_cost", "Logistics Cost", "currencyRUB", data.FieldTypeFloat64, logisticsCosts),
		newTypedField("storage_cost", "Storage Cost", "currencyRUB", data.FieldTypeFloat64, storageCosts),
	)
}

// NewProfitabilityFrame converts product profitability rows into a typed
// Grafana frame. Prices and margins are expressed in RUB; margin percentages
// are expressed from 0 to 100.
func NewProfitabilityFrame(rows []ProfitabilityRow) *data.Frame {
	times := make([]time.Time, len(rows))
	productNames := make([]string, len(rows))
	prices := make([]float64, len(rows))
	netMarginsRUB := make([]float64, len(rows))
	netMarginPcts := make([]float64, len(rows))
	isLosses := make([]bool, len(rows))
	for index, row := range rows {
		times[index] = row.Time.UTC()
		productNames[index] = row.ProductName
		prices[index] = row.Price
		netMarginsRUB[index] = row.NetMarginRUB
		netMarginPcts[index] = row.NetMarginPct
		isLosses[index] = row.IsLoss
	}

	return newTypedFrame("profitability",
		newTypedField("time", "Time", "", data.FieldTypeTime, times),
		newTypedField("product_name", "Product Name", "", data.FieldTypeString, productNames),
		newTypedField("price", "Price", "currencyRUB", data.FieldTypeFloat64, prices),
		newTypedField("net_margin_rub", "Net Margin (RUB)", "currencyRUB", data.FieldTypeFloat64, netMarginsRUB),
		newTypedField("net_margin_pct", "Net Margin (%)", "percent", data.FieldTypeFloat64, netMarginPcts),
		newTypedField("is_loss", "Is Loss", "", data.FieldTypeBool, isLosses),
	)
}

// NewPriceHistoryFrame converts product price history rows into a typed
// Grafana frame. Prices are expressed in RUB.
func NewPriceHistoryFrame(rows []PriceHistoryRow) *data.Frame {
	times := make([]time.Time, len(rows))
	productIDs := make([]string, len(rows))
	prices := make([]float64, len(rows))
	for index, row := range rows {
		times[index] = row.Time.UTC()
		productIDs[index] = row.ProductID
		prices[index] = row.Price
	}

	return newTypedFrame("price_history",
		newTypedField("time", "Time", "", data.FieldTypeTime, times),
		newTypedField("product_id", "Product ID", "", data.FieldTypeString, productIDs),
		newTypedField("price", "Price", "currencyRUB", data.FieldTypeFloat64, prices),
	)
}

func newTypedFrame(name string, fields ...*data.Field) *data.Frame {
	return data.NewFrame(name, fields...)
}

func newTypedField(name, displayName, unit string, fieldType data.FieldType, values interface{}) *data.Field {
	config := &data.FieldConfig{DisplayNameFromDS: displayName}
	if unit != "" {
		config.Unit = unit
	}
	field := data.NewField(name, nil, values).SetConfig(config)
	if field.Type() != fieldType {
		panic("field values do not match the declared Grafana field type")
	}
	return field
}
