package query

import (
	"math"
	"math/big"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type rangeTemporal interface {
	time.Time | temporal.Date | temporal.Time | temporal.DateTime | temporal.LocalDateTime
}

// RangeWindow owns a single ordering value and the type of its RANGE distances.
// Construct it with NumericRange/TemporalRange or their nullable counterparts.
// Between completes it into the ordinary Window used by window functions.
type RangeWindow[S, D any] struct {
	window Window[S]
	order  orderNode
	kind   codec.ParameterType
	encode func(D) rangeDistance
}

// RangeBoundary retains both query scope and concrete distance type. Positions
// are obtained from the same RangeWindow; ordinary ROWS offsets cannot enter it.
type RangeBoundary[S, D any] struct {
	_     [0]*S
	_     [0]*D
	bound frameBound
}

type rangeDistance struct {
	parameter parameterNode
	interval  *temporal.Interval
	err       error
}

// NumericRange orders by a numeric selected value with distances of the same
// concrete Go type. The supplied window must not already specify ordering.
func NumericRange[S any, V numericValue](w Window[S], input Expression[S, V]) RangeWindow[S, V] {
	return numericRange(w, input.node, input.codec.ParameterType(), func(v V) parameterNode {
		return parameterExpression[S](v, input.codec).expression.node.(parameterNode)
	})
}

// NullableNumericRange keeps nullable ordering while requiring non-NULL distances.
func NullableNumericRange[S any, V numericValue](w Window[S], input Expression[S, value.Nullable[V]]) RangeWindow[S, V] {
	return numericRange(w, input.node, input.codec.ParameterType(), func(v V) parameterNode {
		return parameterExpression[S](value.Of(v), input.codec).expression.node.(parameterNode)
	})
}

func numericRange[S any, V numericValue](w Window[S], node valueExpression, kind codec.ParameterType, encode func(V) parameterNode) RangeWindow[S, V] {
	r := RangeWindow[S, V]{window: w, order: orderNode{expression: node}, kind: kind}
	if r.kind != codec.TypeInteger && r.kind != codec.TypeFloat && r.kind != codec.TypeDecimal {
		r.window.node.err = fault.New(fault.Invalid, "numeric RANGE requires a numeric codec representation")
	}
	r.encode = func(v V) rangeDistance {
		return rangeDistance{parameter: encode(v)}
	}
	return r
}

// TemporalRange orders by a date, time or timestamp using calendar/elapsed intervals.
func TemporalRange[S any, V rangeTemporal](w Window[S], input Expression[S, V]) RangeWindow[S, temporal.Interval] {
	return temporalRange(w, input.node, input.codec.ParameterType())
}

// NullableTemporalRange keeps nullable ordering with non-NULL interval distances.
func NullableTemporalRange[S any, V rangeTemporal](w Window[S], input Expression[S, value.Nullable[V]]) RangeWindow[S, temporal.Interval] {
	return temporalRange(w, input.node, input.codec.ParameterType())
}

func temporalRange[S any](w Window[S], node valueExpression, kind codec.ParameterType) RangeWindow[S, temporal.Interval] {
	r := RangeWindow[S, temporal.Interval]{window: w, order: orderNode{expression: node}, kind: kind}
	if kind != codec.TypeDate && kind != codec.TypeTime && kind != codec.TypeDateTime && kind != codec.TypeLocalDateTime {
		r.window.node.err = fault.New(fault.Invalid, "temporal RANGE requires a temporal codec representation")
	}
	r.encode = func(v temporal.Interval) rangeDistance { return rangeDistance{interval: &v} }
	return r
}

// Asc/Desc choose the single RANGE ordering direction. Preceding and following
// are interpreted in that direction, as in PostgreSQL's ordinary window order.
func (r RangeWindow[S, D]) Asc() RangeWindow[S, D]  { r.order.descending = false; return r }
func (r RangeWindow[S, D]) Desc() RangeWindow[S, D] { r.order.descending = true; return r }

func (r RangeWindow[S, D]) position(kind frameBoundKind) RangeBoundary[S, D] {
	return RangeBoundary[S, D]{bound: frameBound{kind: kind}}
}
func (r RangeWindow[S, D]) CurrentRow() RangeBoundary[S, D] { return r.position(currentRow) }
func (r RangeWindow[S, D]) UnboundedPreceding() RangeBoundary[S, D] {
	return r.position(unboundedPreceding)
}
func (r RangeWindow[S, D]) UnboundedFollowing() RangeBoundary[S, D] {
	return r.position(unboundedFollowing)
}

func (r RangeWindow[S, D]) distance(kind frameBoundKind, v D) RangeBoundary[S, D] {
	d := rangeDistance{err: fault.New(fault.Invalid, "undeclared RANGE distance codec")}
	if r.encode != nil {
		d = r.encode(v)
	}
	return RangeBoundary[S, D]{bound: frameBound{kind: kind, distance: &d}}
}

// Preceding/Following capture a concrete distance once. Invalid, negative or
// non-finite values fail query compilation, including in an empty input query.
func (r RangeWindow[S, D]) Preceding(v D) RangeBoundary[S, D] { return r.distance(precedingBound, v) }
func (r RangeWindow[S, D]) Following(v D) RangeBoundary[S, D] { return r.distance(followingBound, v) }

// Between replaces the frame and establishes exactly one ordering expression.
// Existing ordering is an error rather than being silently replaced.
func (r RangeWindow[S, D]) Between(start, end RangeBoundary[S, D]) Window[S] {
	w := r.window
	if r.encode == nil || len(w.node.orders) != 0 {
		w.node.err = fault.New(fault.Invalid, "RANGE requires a declared value and a window without existing ordering")
	}
	w.node.orders = []orderNode{r.order}
	w.node.frame = &windowFrame{kind: rangeFrame, start: start.bound, end: end.bound, rangeType: r.kind}
	return w
}

func (d rangeDistance) validate(kind codec.ParameterType) error {
	if d.err != nil {
		return d.err
	}
	if d.interval != nil {
		if d.parameter.kind != 0 || d.parameter.value != nil || d.parameter.err != nil {
			return fault.New(fault.Invalid, "ambiguous RANGE distance")
		}
		v := *d.interval
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
	if err := d.parameter.validate(); err != nil {
		return err
	}
	if d.parameter.kind != kind {
		return fault.New(fault.Invalid, "RANGE distance representation differs from its ordering")
	}
	switch kind {
	case codec.TypeInteger:
		v, ok := d.parameter.value.(int64)
		if ok && v >= 0 {
			return nil
		}
	case codec.TypeFloat:
		v, ok := d.parameter.value.(float64)
		if ok && v >= 0 && !math.IsInf(v, 0) && !math.IsNaN(v) {
			return nil
		}
	case codec.TypeDecimal:
		var text string
		switch v := d.parameter.value.(type) {
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

func (c *compiler) rangeDistanceSQL(d rangeDistance) (string, error) {
	if d.interval == nil {
		return c.parameterSQL(d.parameter)
	}
	p, err := c.parameter(d.interval.String())
	return "CAST(" + p + " AS interval)", err
}
