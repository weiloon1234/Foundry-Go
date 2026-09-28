package query

import (
	"context"
	"database/sql/driver"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CursorRequest selects a bounded forward or backward page in the query's
// declared order. After and Before are mutually exclusive; omit both to start.
type CursorRequest[M any] struct {
	Size          int
	After, Before value.Optional[Cursor[M]]
}

// CursorPage keeps canonical query order even for backward traversal. Next and
// Previous are navigation hints, not guarantees against concurrent data changes.
type CursorPage[M any] struct {
	Items          []M
	Size           int
	Next, Previous value.Optional[Cursor[M]]
}

type cursorPlan[M any] struct {
	query              Query[M]
	fields             []ModelField[M]
	scope              string
	backward, boundary bool
}

func (q Query[M]) cursorPlan(request CursorRequest[M]) (cursorPlan[M], error) {
	if !validCursorRequest(request) {
		return cursorPlan[M]{}, cursorInputFailure(invalidCursor())
	}
	q, err := q.paginationBase()
	if err != nil {
		return cursorPlan[M]{}, err
	}
	if len(q.orders) > MaxCursorFields {
		return cursorPlan[M]{}, fault.New(fault.Invalid, "cursor ordering exceeds its field bound")
	}
	byName := make(map[string]ModelField[M], len(q.definition.modelFields))
	for _, field := range q.definition.modelFields {
		byName[field.column] = field
	}
	fields := make([]ModelField[M], len(q.orders))
	for i, order := range q.orders {
		if order.computed != nil {
			return cursorPlan[M]{}, fault.New(fault.Invalid, "computed cursor keys require a declared projection and CursorFor")
		}
		field, ok := byName[order.field.column]
		if !ok {
			return cursorPlan[M]{}, fault.New(fault.Invalid, "cursor ordering requires generated field codecs and getters")
		}
		fields[i] = field
	}
	scope, err := cursorScope(q)
	if err != nil {
		return cursorPlan[M]{}, err
	}
	required := make([]bool, len(fields))
	for i, field := range fields {
		required[i] = field.column == q.definition.primary
	}
	navigation, err := readCursorBoundary(request, scope, fields, required)
	if err != nil {
		return cursorPlan[M]{}, err
	}
	q.orders = cursorOrders(q.orders, navigation.backward)
	if navigation.present {
		q = q.Where(cursorPredicate(q.orders, navigation.keys))
	}
	return cursorPlan[M]{query: q.Limit(request.Size + 1), fields: fields, scope: scope, backward: navigation.backward, boundary: navigation.present}, nil
}

// cursorPredicate expands lexicographic comparison with explicit SQL NULL
// semantics. DESC defaults NULLS FIRST, ASC NULLS LAST, including reversed pages.
func cursorPredicate[M any](orders []Order[M], keys []driver.Value) Predicate[M] {
	var alternatives []Predicate[M]
	var prefix []Predicate[M]
	for i, order := range orders {
		key := keys[i]
		compare := func(op operator) Predicate[M] {
			p := comparison{operand: order.field, operator: op}
			if op != isNull && op != isNotNull {
				p.values = []any{key}
				p.bind = func(raw any) (driver.Value, error) { return raw, nil }
			}
			return Predicate[M]{expression: p}
		}
		var after Predicate[M]
		if key == nil {
			if order.descending {
				after = compare(isNotNull)
			}
		} else {
			op := greater
			if order.descending {
				op = less
			}
			after = compare(op)
			if !order.descending {
				after = Or(after, compare(isNull))
			}
		}
		if after.expression != nil {
			terms := append(slices.Clone(prefix), after)
			alternatives = append(alternatives, And(terms...))
		}
		if key == nil {
			prefix = append(prefix, compare(isNull))
		} else {
			prefix = append(prefix, compare(equal))
		}
	}
	if len(alternatives) == 0 {
		// All ascending keys can be NULL for a declared result identity. There
		// is then no following position. Reuse empty membership's FALSE node.
		return Predicate[M]{expression: comparison{operand: orders[0].field, operator: in, bind: func(v any) (driver.Value, error) { return v, nil }}}
	}
	return Or(alternatives...)
}

// CursorPaginate performs one limit-plus-one query, preserving filters and
// supporting mixed directions, nullable sorts and a primary-key tie-breaker.
// Tokens bind to the model, selection, filters and canonical ordering. They do
// not provide authorization or a snapshot across requests.
func (q Query[M]) CursorPaginate(ctx context.Context, executor database.Executor, request CursorRequest[M]) (CursorPage[M], error) {
	if err := executionContext(ctx, executor); err != nil {
		return CursorPage[M]{}, err
	}
	plan, err := q.cursorPlan(request)
	if err != nil {
		return CursorPage[M]{}, err
	}
	items, err := plan.query.allRows(ctx, executor)
	if err != nil {
		return CursorPage[M]{}, err
	}
	items, more, err := plan.query.loadPage(ctx, executor, items, request.Size)
	if err != nil {
		return CursorPage[M]{}, err
	}
	return finishCursorPage(items, request.Size, more, plan.backward, plan.boundary, plan.scope, plan.fields)
}

func finishCursorPage[M any](items []M, size int, more, backward, boundary bool, scope string, fields []RecordField[M]) (CursorPage[M], error) {
	if backward {
		slices.Reverse(items)
	}
	page := CursorPage[M]{Items: items, Size: size}
	if len(items) == 0 {
		return page, nil
	}
	next, previous := more, boundary
	if backward {
		next, previous = boundary, more
	}
	if next {
		cursor, err := makeCursor(scope, fields, items[len(items)-1])
		if err != nil {
			return CursorPage[M]{}, err
		}
		page.Next = value.Set(cursor)
	}
	if previous {
		cursor, err := makeCursor(scope, fields, items[0])
		if err != nil {
			return CursorPage[M]{}, err
		}
		page.Previous = value.Set(cursor)
	}
	return page, nil
}
