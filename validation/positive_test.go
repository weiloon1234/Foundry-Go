package validation_test

import (
	"math"
	"testing"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestPositive(t *testing.T) {
	for _, v := range []float64{0, math.Copysign(0, -1), -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		rejection(t, validation.Check(validation.Positive[float64](), t.Context(), v, validation.DefaultLimits()))
	}
	for _, v := range []int{-1, 0} {
		rejection(t, validation.Check(validation.Positive[int](), t.Context(), v, validation.DefaultLimits()))
	}
	rejection(t, validation.Check(validation.Positive[uint64](), t.Context(), 0, validation.DefaultLimits()))
	for _, v := range []float64{math.SmallestNonzeroFloat64, 1, math.MaxFloat64} {
		if err := validation.Check(validation.Positive[float64](), t.Context(), v, validation.DefaultLimits()); err != nil {
			t.Fatal(err)
		}
	}
	if err := validation.Check(validation.Positive[uint64](), t.Context(), math.MaxUint64, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}

func TestPositiveServerOnly(t *testing.T) {
	requireServerOnly(t, validation.Positive[int](), true)
	requireServerOnly(t, validation.Positive[uint64](), true)
	requireServerOnly(t, validation.Positive[float64](), true)
	requireServerOnly(t, validation.DecimalPositive(), true)
}

func TestDecimalPositive(t *testing.T) {
	for _, input := range []string{"-999999999999999999999999.99", "-0.000000000000000000000001", "-0", "0", "0.000000000000000000000001", "999999999999999999999999.99"} {
		t.Run(input, func(t *testing.T) {
			d, err := decimal.Parse(input)
			if err != nil {
				t.Fatal(err)
			}
			err = validation.Check(validation.DecimalPositive(), t.Context(), d, validation.DefaultLimits())
			if input[0] == '-' || input == "0" {
				rejection(t, err)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
	rejection(t, validation.Check(validation.DecimalPositive(), t.Context(), decimal.Decimal{}, validation.DefaultLimits()))
}
