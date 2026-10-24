package query

import (
	"context"
	"database/sql/driver"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CursorRequest selects a bounded forward or backward page.
type CursorRequest[M any] = querycore.CursorRequest[M]

// CursorPage preserves canonical order and typed navigation hints.
type CursorPage[M any] = querycore.CursorPage[M]

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
	if len(q.queryCore.Orders) > MaxCursorFields {
		return cursorPlan[M]{}, fault.New(fault.Invalid, "cursor ordering exceeds its field bound")
	}
	if err := validateKeysetOrders(q.queryCore.Orders); err != nil {
		return cursorPlan[M]{}, err
	}
	byName := make(map[string]ModelField[M], len(q.definition.modelFields))
	for _, field := range q.definition.modelFields {
		byName[field.column] = field
	}
	fields := make([]ModelField[M], len(q.queryCore.Orders))
	for i, order := range q.queryCore.Orders {
		if order.Computed != nil {
			return cursorPlan[M]{}, fault.New(fault.Invalid, "computed cursor keys require a declared projection and CursorFor")
		}
		field, ok := byName[order.Field.Column]
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
		required[i] = field.column == q.definition.modelDefinition.Primary
	}
	navigation, err := readCursorBoundary(request, scope, fields, required)
	if err != nil {
		return cursorPlan[M]{}, err
	}
	q.queryCore.Orders = cursorOrders(q.queryCore.Orders, navigation.backward)
	if navigation.present {
		q.queryCore.Predicates = append(append([]expression(nil), q.queryCore.Predicates...), cursorPredicate(q.queryCore.Orders, navigation.keys, q.definition.modelDefinition.NullableOrder))
	}
	return cursorPlan[M]{query: q.Limit(request.Size + 1), fields: fields, scope: scope, backward: navigation.backward, boundary: navigation.present}, nil
}

// cursorPredicate delegates the shared NULL-aware keyset predicate.
func cursorPredicate(orders []queryOrder, keys []driver.Value, nullable func(queryOrder) bool) expression {
	return querycore.CursorPredicate(orders, keys, nullable)
}

// anyNullable is the conservative nullability used by result cursors.
func anyNullable(queryOrder) bool { return true }

// validateKeysetOrders rejects explicit NULL placement, which the shared
// keyset predicates do not model.
func validateKeysetOrders(orders []queryOrder) error {
	for _, order := range orders {
		if order.Nulls != nullsDefault {
			return fault.New(fault.Invalid, "cursor and keyset ordering cannot use explicit NULLS FIRST/LAST")
		}
	}
	return nil
}

// CursorPaginate performs one limit-plus-one query, preserving filters and
// supporting mixed directions, nullable sorts and a primary-key tie-breaker.
// Tokens bind to the model, selection, filters and canonical ordering. They do
// not provide authorization or a snapshot across requests.
func (q Query[M]) CursorPaginate(ctx context.Context, executor database.Executor, request CursorRequest[M]) (CursorPage[M], error) {
	if err := executionContext(ctx, executor); err != nil {
		return CursorPage[M]{}, err
	}
	q = q.inContext(ctx)
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
