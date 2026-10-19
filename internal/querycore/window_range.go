package querycore

import (
	"math"
	"math/big"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type RangeDistance struct {
	Parameter ParameterNode
	Interval  *temporal.Interval
	Err       error
}

func (d RangeDistance) Validate(kind codec.ParameterType) error {
	if d.Err != nil {
		return d.Err
	}
	if d.Interval != nil {
		if d.Parameter.Kind != 0 || d.Parameter.Value != nil || d.Parameter.Err != nil {
			return fault.New(fault.Invalid, "ambiguous RANGE distance")
		}
		v := *d.Interval
		if kind == codec.TypeTime {
			// PostgreSQL silently ignores calendar fields for time-of-day ranges.
			// Reject them so a declared day/month cannot accidentally become zero.
			if v.Months() != 0 || v.Days() != 0 || v.Elapsed() < 0 {
				return fault.New(fault.Invalid, "time-of-day RANGE requires a nonnegative elapsed interval")
			}
			return nil
		}
		if kind != codec.TypeDate && kind != codec.TypeDateTime && kind != codec.TypeLocalDateTime {
			return fault.New(fault.Invalid, "interval RANGE requires temporal ordering")
		}
		// PostgreSQL checks interval signs using 30-day months. Retain calendar
		// components for evaluation; this comparison is only its sign check.
		days := int64(v.Months())*30 + int64(v.Days())
		sign := new(big.Int).Mul(big.NewInt(days), big.NewInt(int64(24*time.Hour/time.Microsecond)))
		sign.Add(sign, big.NewInt(int64(v.Elapsed()/time.Microsecond)))
		if sign.Sign() < 0 {
			return fault.New(fault.Invalid, "RANGE distance cannot be negative")
		}
		return nil
	}
	if err := d.Parameter.Validate(); err != nil {
		return err
	}
	if d.Parameter.Kind != kind {
		return fault.New(fault.Invalid, "RANGE distance representation differs from its ordering")
	}
	switch kind {
	case codec.TypeInteger:
		v, ok := d.Parameter.Value.(int64)
		if ok && v >= 0 {
			return nil
		}
	case codec.TypeFloat:
		v, ok := d.Parameter.Value.(float64)
		if ok && v >= 0 && !math.IsInf(v, 0) && !math.IsNaN(v) {
			return nil
		}
	case codec.TypeDecimal:
		var text string
		switch v := d.Parameter.Value.(type) {
		case string:
			text = v
		case []byte:
			text = string(v)
		case int64:
			if v >= 0 {
				return nil
			}
		}
		v, err := decimal.Parse(text)
		if err == nil && v.Cmp(decimal.Decimal{}) >= 0 {
			return nil
		}
	}
	return fault.New(fault.Invalid, "RANGE requires a finite nonnegative distance")
}

func (c *Compiler) RangeDistanceSQL(d RangeDistance) (string, error) {
	if d.Interval == nil {
		return c.ParameterSQL(d.Parameter)
	}
	p, err := c.Parameter(d.Interval.String())
	return "CAST(" + p + " AS interval)", err
}
