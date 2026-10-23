package orm

import (
	"context"
	"database/sql/driver"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CursorPaginate reads a bounded keyset page using the existing HTTP pagination
// request/result adapters. Tokens are scoped positions, not authorization or a
// snapshot. Example: orm.CursorPaginate(ctx, db, Orders.Query(), request.Page).
func CursorPaginate[M any](ctx context.Context, ex database.Executor, q Query[M], request query.CursorRequest[M]) (query.CursorPage[M], error) {
	if err := executionContext(ctx, ex); err != nil {
		return query.CursorPage[M]{}, err
	}
	q.state.ast.ScopeContext = ctx
	if err := request.Validate(); err != nil {
		return query.CursorPage[M]{}, err
	}
	token, present := request.After.Get()
	backward := request.Before.IsSet()
	if backward {
		token, present = request.Before.Get()
	}
	plan, err := newCursorPlan(q.state, request.Size, token.Token(), present, backward)
	if err != nil {
		return query.CursorPage[M]{}, err
	}
	items, err := allRows[M](ctx, ex, plan.query)
	if err != nil {
		return query.CursorPage[M]{}, err
	}
	more := len(items) > request.Size
	if more {
		clear(items[request.Size:])
		items = items[:request.Size:request.Size]
	}
	if err := loadPlans(ctx, ex, unsafe.Pointer(unsafe.SliceData(items)), len(items), plan.query.relations); err != nil {
		return query.CursorPage[M]{}, err
	}
	if backward {
		slices.Reverse(items)
	}
	page := query.CursorPage[M]{Items: items, Size: request.Size}
	if len(items) == 0 {
		return page, nil
	}
	next, previous := more, present
	if backward {
		next, previous = present, more
	}
	if next {
		text, err := plan.token(unsafe.Pointer(&items[len(items)-1]))
		if err != nil {
			return query.CursorPage[M]{}, err
		}
		cursor, err := query.ParseCursor[M](text)
		if err != nil {
			return query.CursorPage[M]{}, err
		}
		page.Next = value.Set(cursor)
	}
	if previous {
		text, err := plan.token(unsafe.Pointer(&items[0]))
		if err != nil {
			return query.CursorPage[M]{}, err
		}
		cursor, err := query.ParseCursor[M](text)
		if err != nil {
			return query.CursorPage[M]{}, err
		}
		page.Previous = value.Set(cursor)
	}
	return page, nil
}

type cursorPlan struct {
	query  queryState
	fields []*columnCore
	scope  string
}

func newCursorPlan(q queryState, size int, token string, present, backward bool) (cursorPlan, error) {
	if err := validateQuery(q); err != nil {
		return cursorPlan{}, err
	}
	if q.ast.Limit.IsSet() || q.ast.Offset != 0 {
		return cursorPlan{}, invalid("pagination requires an unpaginated query")
	}
	var err error
	q.ast, err = q.ast.StableOrder()
	if err != nil {
		return cursorPlan{}, err
	}
	fields := keysetFields(q)
	if fields == nil {
		return cursorPlan{}, invalid("cursor ordering requires bounded plain columns with default NULL placement")
	}
	statement, err := compileRead(q, querycore.ReadModels)
	if err != nil {
		return cursorPlan{}, err
	}
	typ := q.table.typ
	scope, err := querycore.CursorStatementScopeFor(typ.PkgPath()+"/"+typ.String(), statement, nil)
	if err != nil {
		return cursorPlan{}, err
	}
	adapters := make([]querycore.CursorField, len(fields))
	required := make([]bool, len(fields))
	for i, c := range fields {
		adapters[i] = querycore.CursorField{Sensitive: c.sensitive, Decode: func(raw any) (driver.Value, error) {
			captured, err := c.automatic(raw)
			if err != nil {
				return nil, err
			}
			return captured.Bind()
		}}
		required[i] = c.metadata.Name == q.table.model.Primary
	}
	boundary, err := querycore.ReadCursorBoundary(token, present, backward, scope, adapters, required)
	if err != nil {
		return cursorPlan{}, err
	}
	q.ast.Orders = querycore.CursorOrders(q.ast.Orders, backward)
	if present {
		q.ast.Predicates = append(slices.Clone(q.ast.Predicates), querycore.CursorPredicate(q.ast.Orders, boundary.Keys, q.table.model.NullableOrder))
	}
	q.ast.Limit = value.Set(size + 1)
	return cursorPlan{query: q, fields: fields, scope: scope}, nil
}
func (p cursorPlan) token(base unsafe.Pointer) (string, error) {
	return querycore.BuildCursor(p.scope, len(p.fields), func(i int) (driver.Value, error) { return p.fields[i].get(base) })
}
