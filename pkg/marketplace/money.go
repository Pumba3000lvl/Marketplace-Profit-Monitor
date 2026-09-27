package marketplace

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
)

func parseRubles(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("amount is empty")
	}
	rational, ok := new(big.Rat).SetString(value)
	if !ok || rational.Sign() < 0 {
		return 0, fmt.Errorf("invalid non-negative RUB amount %q", value)
	}
	numerator := new(big.Int).Mul(rational.Num(), big.NewInt(100))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, rational.Denom(), remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(rational.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, errors.New("amount exceeds supported kopeck range")
	}
	return quotient.Int64(), nil
}

func rubles(kopecks int64) *float64 {
	value := float64(kopecks) / 100
	return &value
}

func commissionAmount(priceKopecks int64, ratePercent float64) (*int64, error) {
	if math.IsNaN(ratePercent) || math.IsInf(ratePercent, 0) || ratePercent < 0 || ratePercent > 100 {
		return nil, fmt.Errorf("commission rate must be between 0 and 100 percent, got %v", ratePercent)
	}
	basisPoints := int64(math.Round(ratePercent * 100))
	whole := priceKopecks / 10_000 * basisPoints
	remainder := priceKopecks % 10_000 * basisPoints
	amount := whole + remainder/10_000
	if remainder%10_000 >= 5_000 {
		amount++
	}
	return &amount, nil
}

// CalculateNetMargin calculates net margin in RUB using exact kopeck arithmetic.
// Cost price and all three marketplace charges are required; no missing input
// is assumed to be zero.
func CalculateNetMargin(metrics *ProductMetrics) error {
	if metrics == nil {
		return errors.New("product metrics cannot be nil")
	}
	metrics.NetMargin = nil
	metrics.NetMarginPercent = nil
	inputs := []struct {
		name  string
		value *float64
	}{
		{name: "current price", value: metrics.CurrentPrice},
		{name: "commission", value: metrics.Commission},
		{name: "logistics cost", value: metrics.LogisticsCost},
		{name: "storage cost", value: metrics.StorageCost},
		{name: "cost price", value: metrics.CostPrice},
	}
	values := make([]int64, len(inputs))
	for index, input := range inputs {
		if input.value == nil {
			return fmt.Errorf("%s is unavailable", input.name)
		}
		kopecks, err := parseRubles(fmt.Sprintf("%.10f", *input.value))
		if err != nil {
			return fmt.Errorf("invalid %s: %w", input.name, err)
		}
		values[index] = kopecks
	}
	priceKopecks := values[0]
	if priceKopecks == 0 {
		return errors.New("current price must be greater than zero")
	}
	expenses := new(big.Int)
	for _, value := range values[1:] {
		expenses.Add(expenses, big.NewInt(value))
	}
	netMargin := new(big.Int).Sub(big.NewInt(priceKopecks), expenses)
	if !netMargin.IsInt64() {
		return errors.New("net margin exceeds supported kopeck range")
	}
	netMarginKopecks := netMargin.Int64()
	marginRubles := float64(netMarginKopecks) / 100
	marginPercent := float64(netMarginKopecks) / float64(priceKopecks) * 100
	metrics.NetMargin = &marginRubles
	metrics.NetMarginPercent = &marginPercent
	return nil
}
