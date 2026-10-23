package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// GroupKey is a row-phase key for GROUP BY and DISTINCT ON. Example: expr.Group(expr.Value(Users.Email)).
type GroupKey[S any] struct {
	rowKeyPhase [0]struct{}
	_           [0]*S
	core        ormcore.Expression
}

// ProjectionKey is a selected-phase key; it cannot enter GROUP BY. Example: expr.Count[User]().Value().Key().
type ProjectionKey[S any] struct {
	selectedKeyPhase [0]struct{}
	_                [0]*S
	core             ormcore.Expression
}

// SelectedOrder orders selected values through rows, never model queries. Example: count.Desc().
type SelectedOrder[S any] struct {
	selectedOrderPhase [0]struct{}
	_                  [0]*S
	core               ormcore.Order
}

// ProjectionOrder accepts row or selected ordering in a read-only rows query. Example: rows.OrderBy(q,count.Desc()).
type ProjectionOrder[S any] interface{ projectionOrder(*S) ormcore.Order }

func (o Order[S]) projectionOrder(*S) ormcore.Order         { return o.core }
func (o SelectedOrder[S]) projectionOrder(*S) ormcore.Order { return o.core }

// Group constructs a row key without adding methods to columns. Example: expr.Group(expr.Value(Orders.BuyerID)).
func Group[S, V any](v RowValue[S, V]) GroupKey[S] { return GroupKey[S]{core: rowInput(v).data.core} }

// Key constructs a selected key while preserving source ownership. Example: count.Value().Key().
func (e Selected[S, V]) Key() ProjectionKey[S] { return ProjectionKey[S]{core: e.data.core} }

// Asc orders a selected value in a rows query. Example: expr.AddValue(a,b).Asc().
func (e Selected[S, V]) Asc() SelectedOrder[S] { return selectedOrder[S](e.data, false) }

// Desc orders a selected value in a rows query. Example: count.Value().Desc().
func (e Selected[S, V]) Desc() SelectedOrder[S] { return selectedOrder[S](e.data, true) }
func selectedOrder[S, V any](data rowData[V], descending bool) SelectedOrder[S] {
	return SelectedOrder[S]{core: rowOrder[S](data, descending).core}
}

// NullsFirst places SQL NULL first. Example: count.Desc().NullsFirst().
func (o SelectedOrder[S]) NullsFirst() SelectedOrder[S] {
	o.core.Node.Nulls = querycore.NullsFirst
	return o
}

// NullsLast places SQL NULL last. Example: count.Desc().NullsLast().
func (o SelectedOrder[S]) NullsLast() SelectedOrder[S] {
	o.core.Node.Nulls = querycore.NullsLast
	return o
}

// FoundryGroup supplies validated row-key metadata to rows. Example: FoundryGroup(key,sqlowner.Expressions()).
func FoundryGroup[S any](key GroupKey[S], seal sqlowner.ExpressionSeal) ormcore.Expression {
	if !seal.Valid() {
		return ormcore.Expression{Err: ormcore.Invalid("invalid SQL owner capability")}
	}
	return key.core
}

// FoundryKey supplies validated selected-key metadata to rows. Example: FoundryKey(key,sqlowner.Expressions()).
func FoundryKey[S any](key ProjectionKey[S], seal sqlowner.ExpressionSeal) ormcore.Expression {
	if !seal.Valid() {
		return ormcore.Expression{Err: ormcore.Invalid("invalid SQL owner capability")}
	}
	return key.core
}

// FoundryProjectionOrder validates before reading an ordering descriptor. Example: FoundryProjectionOrder(order,sqlowner.Expressions()).
func FoundryProjectionOrder[S any](order ProjectionOrder[S], seal sqlowner.ExpressionSeal) ormcore.Order {
	if !seal.Valid() || ormcore.NilDescriptor(order) {
		return ormcore.Order{Err: ormcore.Invalid("invalid SQL owner capability or ordering")}
	}
	return order.projectionOrder(nil)
}
