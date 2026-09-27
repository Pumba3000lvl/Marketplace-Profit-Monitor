package wildberries

import (
	"errors"
	"fmt"
	"math"
)

const (
	commissionBasisPointScale = int64(10_000)
)

// MarginInput contains the per-item values used to calculate net margin.
// All monetary values are in kopecks. CommissionBasisPoints is an integer
// percentage in basis points: 100 basis points is 1%, and 10,000 is 100%.
// AdCostKopecks is optional and defaults to zero.
type MarginInput struct {
	SalePriceKopecks      int64
	CommissionBasisPoints int64
	LogisticsCostKopecks  int64
	StorageCostKopecks    int64
	CostPriceKopecks      int64
	AdCostKopecks         int64
}

// MarginResult contains the calculated net margin and its expense breakdown.
// NetMarginPercent is a display metric computed from the final integer
// kopeck amounts using float64; it is not used in monetary calculations.
type MarginResult struct {
	SalePriceKopecks     int64
	CommissionKopecks    int64
	LogisticsCostKopecks int64
	StorageCostKopecks   int64
	CostPriceKopecks     int64
	AdCostKopecks        int64
	TotalExpensesKopecks int64
	NetMarginKopecks     int64
	NetMarginPercent     float64
	IsLossMaking         bool
}

// CalculateNetMargin calculates an item's net margin from its sale price and
// expenses. Commission is rounded to the nearest kopeck, with exact half-kopeck
// values rounded up. It returns an error for invalid inputs or expense totals
// that overflow int64.
func CalculateNetMargin(input MarginInput) (MarginResult, error) {
	if input.SalePriceKopecks <= 0 {
		return MarginResult{}, errors.New("sale price must be greater than zero kopecks")
	}
	if input.CommissionBasisPoints < 0 || input.CommissionBasisPoints > commissionBasisPointScale {
		return MarginResult{}, fmt.Errorf("commission must be between 0 and %d basis points", commissionBasisPointScale)
	}

	expenses := []struct {
		name   string
		amount int64
	}{
		{name: "logistics cost", amount: input.LogisticsCostKopecks},
		{name: "storage cost", amount: input.StorageCostKopecks},
		{name: "cost price", amount: input.CostPriceKopecks},
		{name: "ad cost", amount: input.AdCostKopecks},
	}
	for _, expense := range expenses {
		if expense.amount < 0 {
			return MarginResult{}, fmt.Errorf("%s cannot be negative", expense.name)
		}
	}

	commission := calculateCommissionKopecks(input.SalePriceKopecks, input.CommissionBasisPoints)
	totalExpenses := commission
	for _, expense := range expenses {
		if expense.amount > math.MaxInt64-totalExpenses {
			return MarginResult{}, errors.New("total expenses overflow int64")
		}
		totalExpenses += expense.amount
	}

	netMargin := input.SalePriceKopecks - totalExpenses
	return MarginResult{
		SalePriceKopecks:     input.SalePriceKopecks,
		CommissionKopecks:    commission,
		LogisticsCostKopecks: input.LogisticsCostKopecks,
		StorageCostKopecks:   input.StorageCostKopecks,
		CostPriceKopecks:     input.CostPriceKopecks,
		AdCostKopecks:        input.AdCostKopecks,
		TotalExpensesKopecks: totalExpenses,
		NetMarginKopecks:     netMargin,
		NetMarginPercent:     float64(netMargin) / float64(input.SalePriceKopecks) * 100,
		IsLossMaking:         netMargin < 0,
	}, nil
}

func calculateCommissionKopecks(salePriceKopecks, commissionBasisPoints int64) int64 {
	whole := salePriceKopecks / commissionBasisPointScale * commissionBasisPoints
	remainder := salePriceKopecks % commissionBasisPointScale * commissionBasisPoints
	commission := whole + remainder/commissionBasisPointScale
	if remainder%commissionBasisPointScale >= commissionBasisPointScale/2 {
		commission++
	}
	return commission
}
