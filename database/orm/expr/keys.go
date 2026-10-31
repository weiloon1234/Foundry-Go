package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// GroupKey is a row-phase key for GROUP BY and DISTINCT ON.
//
// Example: expr.Group(expr.Value(orm.TextExpr(Users.Email)))
type GroupKey[S any] struct {
	rowKeyPhase [0]struct{}
	_           [0]*S
	core        ormcore.Expression
}

// ProjectionKey is a selected-phase key; it cannot enter GROUP BY.
//
// Example: expr.Key(expr.AsSelected(expr.Count[User]()))
type ProjectionKey[S any] = projectionKey[*S]

type projectionKey[S any] struct {
	selectedKeyPhase [0]struct{}
	_                [0]S
	core             ormcore.Expression
}

// SelectedOrder orders selected values through rows, never model queries.
//
// Example: expr.DescValue(count)
type SelectedOrder[S any] struct {
	selectedOrderPhase [0]struct{}
	_                  [0]*S
	core               ormcore.Order
}

// ProjectionOrder accepts row or selected ordering in a read-only rows query.
//
// Example: rows.OrderBy(q, expr.DescValue(count))
type ProjectionOrder[S any] interface{ projectionOrder(*S) ormcore.Order }

func (o Order[S]) projectionOrder(*S) ormcore.Order         { return o.core }
func (o SelectedOrder[S]) projectionOrder(*S) ormcore.Order { return o.core }

// Group constructs a row key without adding methods to columns.
//
// Example: expr.Group(v)
func Group[S, V any](v RowValue[S, V]) GroupKey[S] { return GroupKey[S]{core: rowInput(v).data.core} }

// NullsFirst places SQL NULL first.
//
// Example: expr.DescValue(count).NullsFirst()
func (o SelectedOrder[S]) NullsFirst() SelectedOrder[S] {
	o.core.Node.Nulls = querycore.NullsFirst
	return o
}

// NullsLast places SQL NULL last.
//
// Example: expr.DescValue(count).NullsLast()
func (o SelectedOrder[S]) NullsLast() SelectedOrder[S] {
	o.core.Node.Nulls = querycore.NullsLast
	return o
}

// FoundryGroup supplies validated row-key metadata to rows.
//
// Example: expr.FoundryGroup(key, seal)
func FoundryGroup[S any](key GroupKey[S], seal sqlowner.ExpressionSeal) ormcore.Expression {
	if !seal.Valid() {
		return ormcore.Expression{Err: ormcore.Invalid("invalid SQL owner capability")}
	}
	return key.core
}

// FoundryKey supplies validated selected-key metadata to rows.
//
// Example: expr.FoundryKey(key, seal)
func FoundryKey[S any](key ProjectionKey[S], seal sqlowner.ExpressionSeal) ormcore.Expression {
	if !seal.Valid() {
		return ormcore.Expression{Err: ormcore.Invalid("invalid SQL owner capability")}
	}
	return key.core
}

// FoundryProjectionOrder validates before reading an ordering descriptor.
//
// Example: expr.FoundryProjectionOrder(order, seal)
func FoundryProjectionOrder[S any](order ProjectionOrder[S], seal sqlowner.ExpressionSeal) ormcore.Order {
	if !seal.Valid() || ormcore.NilDescriptor(order) {
		return ormcore.Order{Err: ormcore.Invalid("invalid SQL owner capability or ordering")}
	}
	return order.projectionOrder(nil)
}
