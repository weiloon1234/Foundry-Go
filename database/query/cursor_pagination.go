package query

import (
	"context"
	"database/sql/driver"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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
	if len(q.queryCore.Orders) > MaxCursorFields {
		return cursorPlan[M]{}, safeerror.NewFault(fault.Invalid, "cursor ordering exceeds its field bound")
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
			return cursorPlan[M]{}, safeerror.NewFault(fault.Invalid, "computed cursor keys require a declared projection and CursorFor")
		}
		field, ok := byName[order.Field.Column]
		if !ok {
			return cursorPlan[M]{}, safeerror.NewFault(fault.Invalid, "cursor ordering requires generated field codecs and getters")
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

// cursorPredicate expands lexicographic comparison with explicit SQL NULL
// semantics. DESC defaults NULLS FIRST, ASC NULLS LAST, including reversed pages.
// nullable reports whether an order key can be NULL; NOT NULL keys never add an
// IS NULL alternative. When every key is NOT NULL and all keys share one
// direction, the equivalent row comparison (a, id) > ($1, $2) is emitted so
// PostgreSQL can use one composite index range.
func cursorPredicate(orders []queryOrder, keys []driver.Value, nullable func(queryOrder) bool) expression {
	if rowKeyset(orders, keys, nullable) {
		operands := make([]valueExpression, len(orders))
		for i, order := range orders {
			operands[i] = order.Field
		}
		op := greater
		if orders[0].Descending {
			op = less
		}
		return rowComparison{Operands: operands, Operator: op, Values: slices.Clone(keys)}
	}
	var alternatives []expression
	var prefix []expression
	for i, order := range orders {
		key := keys[i]
		compare := func(op operator) expression {
			p := comparison{Operand: order.Field, Operator: op}
			if op != isNull && op != isNotNull {
				p.Values = []any{key}
				p.Bind = func(raw any) (driver.Value, error) { return raw, nil }
			}
			return p
		}
		var after expression
		if key == nil {
			if order.Descending {
				after = compare(isNotNull)
			}
		} else {
			op := greater
			if order.Descending {
				op = less
			}
			after = compare(op)
			if !order.Descending && nullable(order) {
				after = junction{Any: true, Children: []expression{after, compare(isNull)}}
			}
		}
		if after != nil {
			terms := append(slices.Clone(prefix), after)
			alternatives = append(alternatives, junction{Children: terms})
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
		return comparison{Operand: orders[0].Field, Operator: in, Bind: func(v any) (driver.Value, error) { return v, nil }}
	}
	return junction{Any: true, Children: alternatives}
}

// rowKeyset allows a row comparison only for two or more plain NOT NULL keys
// with present values, one shared direction and default NULL placement.
func rowKeyset(orders []queryOrder, keys []driver.Value, nullable func(queryOrder) bool) bool {
	if len(orders) < 2 || len(orders) != len(keys) {
		return false
	}
	for i, order := range orders {
		if order.Computed != nil || order.Nulls != nullsDefault || order.Descending != orders[0].Descending || keys[i] == nil || nullable(order) {
			return false
		}
	}
	return true
}

// anyNullable is the conservative nullability used by result cursors.
func anyNullable(queryOrder) bool { return true }

// validateKeysetOrders rejects explicit NULL placement, which the shared
// keyset predicates do not model.
func validateKeysetOrders(orders []queryOrder) error {
	for _, order := range orders {
		if order.Nulls != nullsDefault {
			return safeerror.NewFault(fault.Invalid, "cursor and keyset ordering cannot use explicit NULLS FIRST/LAST")
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
