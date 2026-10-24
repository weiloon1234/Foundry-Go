package query

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type rangeDistance = querycore.RangeDistance

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
	r := RangeWindow[S, V]{window: w, order: orderNode{Expression: node}, kind: kind}
	if err := querycore.ValidateRangeOrder(r.kind, false); err != nil {
		r.window.node.Err = err
	}
	r.encode = func(v V) rangeDistance {
		return rangeDistance{Parameter: encode(v)}
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
	r := RangeWindow[S, temporal.Interval]{window: w, order: orderNode{Expression: node}, kind: kind}
	if err := querycore.ValidateRangeOrder(kind, true); err != nil {
		r.window.node.Err = err
	}
	r.encode = func(v temporal.Interval) rangeDistance { return rangeDistance{Interval: &v} }
	return r
}

// Asc/Desc choose the single RANGE ordering direction. Preceding and following
// are interpreted in that direction, as in PostgreSQL's ordinary window order.
func (r RangeWindow[S, D]) Asc() RangeWindow[S, D] { r.order.Descending = false; return r }

func (r RangeWindow[S, D]) Desc() RangeWindow[S, D] { r.order.Descending = true; return r }

func (r RangeWindow[S, D]) position(kind frameBoundKind) RangeBoundary[S, D] {
	return RangeBoundary[S, D]{bound: frameBound{Kind: kind}}
}

func (r RangeWindow[S, D]) CurrentRow() RangeBoundary[S, D] { return r.position(currentRow) }

func (r RangeWindow[S, D]) UnboundedPreceding() RangeBoundary[S, D] {
	return r.position(unboundedPreceding)
}

func (r RangeWindow[S, D]) UnboundedFollowing() RangeBoundary[S, D] {
	return r.position(unboundedFollowing)
}

func (r RangeWindow[S, D]) distance(kind frameBoundKind, v D) RangeBoundary[S, D] {
	d := rangeDistance{Err: fault.New(fault.Invalid, "undeclared RANGE distance codec")}
	if r.encode != nil {
		d = r.encode(v)
	}
	return RangeBoundary[S, D]{bound: frameBound{Kind: kind, Distance: &d}}
}

// Preceding/Following capture a concrete distance once. Invalid, negative or
// non-finite values fail query compilation, including in an empty input query.
func (r RangeWindow[S, D]) Preceding(v D) RangeBoundary[S, D] { return r.distance(precedingBound, v) }

func (r RangeWindow[S, D]) Following(v D) RangeBoundary[S, D] { return r.distance(followingBound, v) }

// Between replaces the frame and establishes exactly one ordering expression.
// Existing ordering is an error rather than being silently replaced.
func (r RangeWindow[S, D]) Between(start, end RangeBoundary[S, D]) Window[S] {
	w := r.window
	if r.encode == nil || len(w.node.Orders) != 0 {
		w.node.Err = fault.New(fault.Invalid, "RANGE requires a declared value and a window without existing ordering")
	}
	w.node.Orders = []orderNode{r.order}
	w.node.Frame = &windowFrame{Kind: rangeFrame, Start: start.bound, End: end.bound, RangeType: r.kind}
	return w
}
