package rows

import (
	"context"
	"reflect"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CursorScope identifies completed output independently of the input scope.
// Example: expr.At(cursor.Scope(), orm.KeyExpr(Users.ID)).
type CursorScope[R any] struct{ _ [0]*R }

// CursorQuery pages complete results using an explicit unique output identity.
// Example: rows.CursorFor(rows.ModelRecord(Users.Query())).
type CursorQuery[R any] struct {
	_    [0]*R
	core ormcore.ResultCursor
}

// CursorFor retains filters and windows behind a completed-result boundary.
// Example: rows.CursorFor(rows.RecordOf(report)).UniqueBy(expr.Group(id)).
func CursorFor[R any](source RecordSource[R]) CursorQuery[R] {
	return CursorQuery[R]{core: ormcore.NewResultCursor(recordOf(source), reflect.TypeFor[CursorScope[R]]())}
}

// ValueCursorQuery pages one exact scalar type, including nullable results.
// Example: rows.ValueCursorFor(rows.Values(selected)).
type ValueCursorQuery[V any] struct {
	_      [0]*V
	core   ormcore.ResultCursor
	column ormcore.Column[V]
}

// ValueCursorFor preserves duplicates; UniqueBy is the caller's uniqueness assertion.
// Example: rows.ValueCursorFor(rows.UnionValues(a,b)).
func ValueCursorFor[V any](source ValueSource[V]) ValueCursorQuery[V] {
	r, c := valueSource(source)
	return ValueCursorQuery[V]{core: ormcore.NewResultCursor(r, reflect.TypeFor[CursorScope[V]]()), column: c}
}

// Value exposes the completed scalar for filtering and ordering. Example: c.Value().Asc().
func (q ValueCursorQuery[V]) Value() expr.RowExpression[CursorScope[V], V] {
	r := q.core.Record
	ref := ormcore.Reference{Table: r.Source.Node.Source.Name(), Owner: r.Record.Owner}
	return expr.FoundryRowExpression[CursorScope[V]](ormcore.Expression{Node: querycore.FieldRef{Table: ref.Table, Column: "value"}, Refs: []ormcore.Reference{ref}, Err: r.Source.Err}, q.column, sqlowner.Expressions())
}

// Scope exposes completed output fields. Example: expr.At(c.Scope(), orm.KeyExpr(Users.ID)).
func (q CursorQuery[R]) Scope() expr.Scope[CursorScope[R], R] {
	r := q.core.Record
	return expr.FoundryScope[CursorScope[R], R](ormcore.Scope{Reference: ormcore.Reference{Table: r.Source.Node.Source.Name(), Owner: r.Record.Owner}, Err: r.Source.Err}, sqlowner.Expressions())
}

// Where filters completed rows. Example: c.Where(id.Ne(zero)).
func (q CursorQuery[R]) Where(ps ...expr.Predicate[CursorScope[R]]) CursorQuery[R] {
	q.core.Record.Source = whereSource(q.core.Record.Source, ps)
	return q
}

// OrderBy orders plain output fields. Example: c.OrderBy(id.Asc()).
func (q CursorQuery[R]) OrderBy(os ...expr.ProjectionOrder[CursorScope[R]]) CursorQuery[R] {
	q.core = cursorOrders(q.core, os)
	return q
}

// UniqueBy asserts result uniqueness and appends missing identity orders.
// Example: c.UniqueBy(expr.Group(id)).
func (q CursorQuery[R]) UniqueBy(keys ...expr.GroupKey[CursorScope[R]]) CursorQuery[R] {
	q.core = cursorKeys(q.core, keys)
	return q
}

// Paginate reads one lookahead row; tokens are positions, not authorization.
// Example: c.Paginate(ctx,db,orm.CursorRequest[R]{Size:20}).
func (q CursorQuery[R]) Paginate(ctx context.Context, ex database.Executor, r orm.CursorRequest[R]) (orm.CursorPage[R], error) {
	return cursorPage(ctx, ex, q.core, r)
}

// Scope exposes completed output fields. Example: expr.At(c.Scope(), orm.KeyExpr(Users.ID)).
func (q ValueCursorQuery[V]) Scope() expr.Scope[CursorScope[V], V] {
	r := q.core.Record
	return expr.FoundryScope[CursorScope[V], V](ormcore.Scope{Reference: ormcore.Reference{Table: r.Source.Node.Source.Name(), Owner: r.Record.Owner}, Err: r.Source.Err}, sqlowner.Expressions())
}

// Where filters completed rows. Example: c.Where(id.Ne(zero)).
func (q ValueCursorQuery[V]) Where(ps ...expr.Predicate[CursorScope[V]]) ValueCursorQuery[V] {
	q.core.Record.Source = whereSource(q.core.Record.Source, ps)
	return q
}

// OrderBy orders plain output fields. Example: c.OrderBy(id.Asc()).
func (q ValueCursorQuery[V]) OrderBy(os ...expr.ProjectionOrder[CursorScope[V]]) ValueCursorQuery[V] {
	q.core = cursorOrders(q.core, os)
	return q
}

// UniqueBy asserts result uniqueness and appends missing identity orders.
// Example: c.UniqueBy(expr.Group(id)).
func (q ValueCursorQuery[V]) UniqueBy(keys ...expr.GroupKey[CursorScope[V]]) ValueCursorQuery[V] {
	q.core = cursorKeys(q.core, keys)
	return q
}

// Paginate reads one lookahead row; tokens are positions, not authorization.
// Example: c.Paginate(ctx,db,orm.CursorRequest[V]{Size:20}).
func (q ValueCursorQuery[V]) Paginate(ctx context.Context, ex database.Executor, r orm.CursorRequest[V]) (orm.CursorPage[V], error) {
	return cursorPage(ctx, ex, q.core, r)
}

func cursorOrders[S any](q ormcore.ResultCursor, os []expr.ProjectionOrder[S]) ormcore.ResultCursor {
	orders := make([]ormcore.Order, len(os))
	for i, o := range os {
		orders[i] = expr.FoundryProjectionOrder(o, sqlowner.Expressions())
	}
	return ormcore.CursorOrder(q, orders)
}
func cursorKeys[S any](q ormcore.ResultCursor, keys []expr.GroupKey[S]) ormcore.ResultCursor {
	inputs := make([]ormcore.Expression, len(keys))
	for i, k := range keys {
		inputs[i] = expr.FoundryGroup(k, sqlowner.Expressions())
	}
	return ormcore.CursorUnique(q, inputs)
}
func cursorPage[R any](ctx context.Context, ex database.Executor, q ormcore.ResultCursor, request orm.CursorRequest[R]) (orm.CursorPage[R], error) {
	if ctx == nil || ormcore.NilDescriptor(ex) {
		return orm.CursorPage[R]{}, ormcore.Invalid("cursor requires a context and executor")
	}
	if err := ctx.Err(); err != nil {
		return orm.CursorPage[R]{}, err
	}
	if err := request.Validate(); err != nil {
		return orm.CursorPage[R]{}, err
	}
	token, present := request.After.Get()
	backward := request.Before.IsSet()
	if backward {
		token, present = request.Before.Get()
	}
	plan, err := ormcore.PlanResultCursor(ctx, q, request.Size, token.Token(), present, backward)
	if err != nil {
		return orm.CursorPage[R]{}, err
	}
	items, err := readRecord[R](ctx, ex, plan.Record, false)
	if err != nil {
		return orm.CursorPage[R]{}, err
	}
	more := len(items) > request.Size
	if more {
		clear(items[request.Size:])
		items = items[:request.Size:request.Size]
	}
	if backward {
		slices.Reverse(items)
	}
	page := orm.CursorPage[R]{Items: items, Size: request.Size}
	if len(items) == 0 {
		return page, nil
	}
	next, previous := more, present
	if backward {
		next, previous = present, more
	}
	makeToken := func(item *R) (orm.Cursor[R], error) {
		token, err := plan.Token(unsafe.Pointer(item))
		if err != nil {
			return orm.Cursor[R]{}, err
		}
		return orm.ParseCursor[R](token)
	}
	if next {
		c, err := makeToken(&items[len(items)-1])
		if err != nil {
			return orm.CursorPage[R]{}, err
		}
		page.Next = value.Set(c)
	}
	if previous {
		c, err := makeToken(&items[0])
		if err != nil {
			return orm.CursorPage[R]{}, err
		}
		page.Previous = value.Set(c)
	}
	return page, nil
}
