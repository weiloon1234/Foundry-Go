package expr

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type rangeTemporal interface {
	time.Time | temporal.Date | temporal.Time | temporal.DateTime | temporal.LocalDateTime
}

// RangeWindow fixes one order value and its typed distance. Example: expr.NumericRange(w,expr.Select(amount)).
type RangeWindow[S, D any] struct {
	_      [0]*S
	data   ormcore.RangeWindow
	encode func(D) querycore.RangeDistance
}

// RangeBoundary retains scope and distance type. Example: r.Preceding(2).
type RangeBoundary[S, D any] struct {
	_     [0]*S
	_     [0]*D
	bound querycore.FrameBound
}

// NumericRange uses the order value's exact numeric type for distances. Example: expr.NumericRange(w,expr.Select(amount)).
func NumericRange[S any, V numericValue](w Window[S], input Selected[S, V]) RangeWindow[S, V] {
	return RangeWindow[S, V]{data: ormcore.WindowRange(ownedWindow(w), input.data.core, input.data.column.Kind, false), encode: func(v V) querycore.RangeDistance {
		return querycore.RangeDistance{Parameter: parameter[S](input.data, v).data.core.Node.(querycore.ParameterNode)}
	}}
}

// NullableNumericRange orders nullable values using present distances. Example: expr.NullableNumericRange(w,expr.Select(optional)).
func NullableNumericRange[S any, V numericValue](w Window[S], input Selected[S, value.Nullable[V]]) RangeWindow[S, V] {
	return RangeWindow[S, V]{data: ormcore.WindowRange(ownedWindow(w), input.data.core, input.data.column.Kind, false), encode: func(v V) querycore.RangeDistance {
		return querycore.RangeDistance{Parameter: parameter[S](input.data, value.Of(v)).data.core.Node.(querycore.ParameterNode)}
	}}
}

// TemporalRange uses calendar/elapsed intervals for temporal distances. Example: expr.TemporalRange(w,expr.Select(created)).
func TemporalRange[S any, V rangeTemporal](w Window[S], input Selected[S, V]) RangeWindow[S, temporal.Interval] {
	return RangeWindow[S, temporal.Interval]{data: ormcore.WindowRange(ownedWindow(w), input.data.core, input.data.column.Kind, true), encode: intervalDistance}
}

// NullableTemporalRange keeps nullable ordering and present interval distances. Example: expr.NullableTemporalRange(w,expr.Select(birthday)).
func NullableTemporalRange[S any, V rangeTemporal](w Window[S], input Selected[S, value.Nullable[V]]) RangeWindow[S, temporal.Interval] {
	return RangeWindow[S, temporal.Interval]{data: ormcore.WindowRange(ownedWindow(w), input.data.core, input.data.column.Kind, true), encode: intervalDistance}
}
func intervalDistance(v temporal.Interval) querycore.RangeDistance {
	return querycore.RangeDistance{Interval: &v}
}

// Asc orders RANGE values ascending. Example: r.Asc().
func (r RangeWindow[S, D]) Asc() RangeWindow[S, D] { r.data.Order.Descending = false; return r }

// Desc orders RANGE values descending. Example: r.Desc().
func (r RangeWindow[S, D]) Desc() RangeWindow[S, D] { r.data.Order.Descending = true; return r }

// CurrentRow identifies the current peers. Example: r.CurrentRow().
func (r RangeWindow[S, D]) CurrentRow() RangeBoundary[S, D] {
	return RangeBoundary[S, D]{bound: querycore.FrameBound{Kind: querycore.CurrentRow}}
}

// UnboundedPreceding identifies the first partition row. Example: r.UnboundedPreceding().
func (r RangeWindow[S, D]) UnboundedPreceding() RangeBoundary[S, D] {
	return RangeBoundary[S, D]{bound: querycore.FrameBound{Kind: querycore.UnboundedPreceding}}
}

// UnboundedFollowing identifies the last partition row. Example: r.UnboundedFollowing().
func (r RangeWindow[S, D]) UnboundedFollowing() RangeBoundary[S, D] {
	return RangeBoundary[S, D]{bound: querycore.FrameBound{Kind: querycore.UnboundedFollowing}}
}
func (r RangeWindow[S, D]) distance(kind querycore.FrameBoundKind, v D) RangeBoundary[S, D] {
	d := querycore.RangeDistance{Err: ormcore.Invalid("undeclared RANGE distance codec")}
	if r.encode != nil {
		d = r.encode(v)
	}
	return RangeBoundary[S, D]{bound: querycore.FrameBound{Kind: kind, Distance: &d}}
}

// Preceding captures a nonnegative finite distance once. Example: r.Preceding(2).
func (r RangeWindow[S, D]) Preceding(v D) RangeBoundary[S, D] {
	return r.distance(querycore.PrecedingBound, v)
}

// Following captures a nonnegative finite distance once. Example: r.Following(2).
func (r RangeWindow[S, D]) Following(v D) RangeBoundary[S, D] {
	return r.distance(querycore.FollowingBound, v)
}

// Between completes the frame; preexisting window ordering is invalid. Example: r.Between(r.Preceding(2),r.CurrentRow()).
func (r RangeWindow[S, D]) Between(start, end RangeBoundary[S, D]) Window[S] {
	return Window[S]{data: ormcore.RangeBetween(r.data, r.encode != nil, start.bound, end.bound)}
}
