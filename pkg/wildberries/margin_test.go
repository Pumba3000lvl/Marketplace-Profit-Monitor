package wildberries

import (
	"fmt"
	"math"
	"testing"
)

func TestCalculateNetMargin(t *testing.T) {
	tests := []struct {
		name           string
		input          MarginInput
		wantCommission int64
		wantExpenses   int64
		wantMargin     int64
		wantPercent    float64
		wantLossMaking bool
	}{
		{
			name: "profitable sale with all expenses",
			input: MarginInput{
				SalePriceKopecks:      10_000,
				CommissionBasisPoints: 1_500,
				LogisticsCostKopecks:  500,
				StorageCostKopecks:    100,
				CostPriceKopecks:      3_000,
				AdCostKopecks:         200,
			},
			wantCommission: 1_500,
			wantExpenses:   5_300,
			wantMargin:     4_700,
			wantPercent:    47,
		},
		{
			name: "ad cost omitted",
			input: MarginInput{
				SalePriceKopecks: 100,
			},
			wantMargin:  100,
			wantPercent: 100,
		},
		{
			name: "ad cost explicitly zero",
			input: MarginInput{
				SalePriceKopecks:      100,
				CommissionBasisPoints: 1_000,
			},
			wantCommission: 10,
			wantExpenses:   10,
			wantMargin:     90,
			wantPercent:    90,
		},
		{
			name: "zero cost price omits cost of goods",
			input: MarginInput{
				SalePriceKopecks:      10_000,
				CommissionBasisPoints: 1_000,
				LogisticsCostKopecks:  200,
				StorageCostKopecks:    100,
			},
			wantCommission: 1_000,
			wantExpenses:   1_300,
			wantMargin:     8_700,
			wantPercent:    87,
		},
		{
			name: "100 percent commission",
			input: MarginInput{
				SalePriceKopecks:      10_000,
				CommissionBasisPoints: 10_000,
			},
			wantCommission: 10_000,
			wantExpenses:   10_000,
			wantPercent:    0,
		},
		{
			name: "loss making",
			input: MarginInput{
				SalePriceKopecks:      1_000,
				CommissionBasisPoints: 1_000,
				LogisticsCostKopecks:  1_000,
			},
			wantCommission: 100,
			wantExpenses:   1_100,
			wantMargin:     -100,
			wantPercent:    -10,
			wantLossMaking: true,
		},
		{
			name: "half kopeck rounds up",
			input: MarginInput{
				SalePriceKopecks:      3,
				CommissionBasisPoints: 5_000,
			},
			wantCommission: 2,
			wantExpenses:   2,
			wantMargin:     1,
			wantPercent:    100.0 / 3,
		},
		{
			name: "below half kopeck rounds down",
			input: MarginInput{
				SalePriceKopecks:      1,
				CommissionBasisPoints: 4_999,
			},
			wantPercent: 100,
			wantMargin:  1,
		},
		{
			name: "exact half kopeck rounds up",
			input: MarginInput{
				SalePriceKopecks:      1,
				CommissionBasisPoints: 5_000,
			},
			wantCommission: 1,
			wantExpenses:   1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := CalculateNetMargin(test.input)
			if err != nil {
				t.Fatalf("CalculateNetMargin() error = %v", err)
			}
			if got.SalePriceKopecks != test.input.SalePriceKopecks ||
				got.CommissionKopecks != test.wantCommission ||
				got.LogisticsCostKopecks != test.input.LogisticsCostKopecks ||
				got.StorageCostKopecks != test.input.StorageCostKopecks ||
				got.CostPriceKopecks != test.input.CostPriceKopecks ||
				got.AdCostKopecks != test.input.AdCostKopecks ||
				got.TotalExpensesKopecks != test.wantExpenses ||
				got.NetMarginKopecks != test.wantMargin ||
				math.Abs(got.NetMarginPercent-test.wantPercent) > 1e-12 ||
				got.IsLossMaking != test.wantLossMaking {
				t.Errorf("CalculateNetMargin() = %+v, want commission=%d expenses=%d margin=%d percent=%v loss=%t",
					got, test.wantCommission, test.wantExpenses, test.wantMargin, test.wantPercent, test.wantLossMaking)
			}
		})
	}
}

func TestCalculateNetMarginRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name  string
		input MarginInput
	}{
		{name: "zero sale price", input: MarginInput{SalePriceKopecks: 0}},
		{name: "negative sale price", input: MarginInput{SalePriceKopecks: -1}},
		{name: "negative commission", input: MarginInput{SalePriceKopecks: 1, CommissionBasisPoints: -1}},
		{name: "commission above 100 percent", input: MarginInput{SalePriceKopecks: 1, CommissionBasisPoints: 10_001}},
		{name: "negative logistics cost", input: MarginInput{SalePriceKopecks: 1, LogisticsCostKopecks: -1}},
		{name: "negative storage cost", input: MarginInput{SalePriceKopecks: 1, StorageCostKopecks: -1}},
		{name: "negative cost price", input: MarginInput{SalePriceKopecks: 1, CostPriceKopecks: -1}},
		{name: "negative ad cost", input: MarginInput{SalePriceKopecks: 1, AdCostKopecks: -1}},
		{
			name: "total expenses overflow",
			input: MarginInput{
				SalePriceKopecks:     1,
				LogisticsCostKopecks: math.MaxInt64,
				StorageCostKopecks:   1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CalculateNetMargin(test.input); err == nil {
				t.Fatal("CalculateNetMargin() succeeded, want error")
			}
		})
	}
}

func ExampleCalculateNetMargin() {
	result, err := CalculateNetMargin(MarginInput{
		SalePriceKopecks:      10_000,
		CommissionBasisPoints: 1_500,
		LogisticsCostKopecks:  500,
		StorageCostKopecks:    100,
		CostPriceKopecks:      3_000,
		AdCostKopecks:         200,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("net margin: %d kopecks (%.2f%%)\n", result.NetMarginKopecks, result.NetMarginPercent)
	fmt.Printf("commission: %d kopecks; total expenses: %d kopecks\n",
		result.CommissionKopecks, result.TotalExpensesKopecks)
	// Output:
	// net margin: 4700 kopecks (47.00%)
	// commission: 1500 kopecks; total expenses: 5300 kopecks
}
