package query

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CursorScope identifies the completed result, independently of its input
// tables, grouping or set scopes. Use Scope with generated FieldsAt functions.
type CursorScope[R any] struct{ _ [0]*R }

// CursorQuery pages a complete result through its typed output fields. UniqueBy
// declares the result identity; arbitrary SQL result uniqueness cannot be
// inferred from a model key after joins/grouping/set operations.
type CursorQuery[R any] struct {
	node     selectNode
	metadata *recordMetadata[R]
	orders   []Order[CursorScope[R]]
	unique   []fieldRef
	err      error
}

// CursorFor creates a cursor boundary around a complete model/projection/CTE/set
// result. Existing source filters, ordering, limits and offsets remain inside
// that boundary. Outer ordering is declared separately through the result's Scope.
// Models needing eager loading use their ordinary CursorPaginate method instead.
func CursorFor[R any](source RecordQuerySource[R]) CursorQuery[R] {
	if nilDescriptor(source) {
		return CursorQuery[R]{err: fault.New(fault.Invalid, "cursor result requires a complete source")}
	}
	return cursorForRecord(source.recordQuery())
}

func cursorForRecord[R any](source recordQuery[R]) CursorQuery[R] {
	q := CursorQuery[R]{err: source.err, metadata: source.metadata()}
	if q.err != nil {
		return q
	}
	if len(source.columns) == 0 || len(source.columns) != len(source.node.selections) || len(source.columns) > MaxExpressionNodes || source.scan == nil {
		q.err = fault.New(fault.Invalid, "cursor result requires bounded columns and a complete decoder")
		return q
	}
	names, err := namesInSelects(source.node)
	if err != nil {
		q.err = err
		return q
	}
	alias := names.allocate("foundry_cursor")
	inner := source.node
	inner.selections = slices.Clone(inner.selections)
	for i, column := range source.columns {
		inner.selections[i].alias = column.Name
	}
	q.node = selectNode{source: tableSource{query: &inner, alias: alias, columns: source.columns}, selections: columnSelections(alias, source.columns)}
	return q
}

// Scope exposes generated, typed output fields for cursor ordering and identity.
func (q CursorQuery[R]) Scope() RecordScope[CursorScope[R], R] {
	if q.err != nil || q.metadata == nil {
		return RecordScope[CursorScope[R], R]{}
	}
	return RecordScope[CursorScope[R], R]{table: q.node.source.name(), record: q.metadata}
}

// Where filters completed output rows; source filters/grouping remain inside.
func (q CursorQuery[R]) Where(predicates ...Predicate[CursorScope[R]]) CursorQuery[R] {
	q.node.predicates = appendPredicates(q.node.predicates, predicates)
	return q
}

// OrderBy appends output-field ordering. Select computed sort expressions into
// the declared result before CursorFor. Repeated/foreign fields are rejected.
func (q CursorQuery[R]) OrderBy(orders ...ProjectionOrder[CursorScope[R]]) CursorQuery[R] {
	q.orders = slices.Clone(q.orders)
	for _, order := range orders {
		if nilDescriptor(order) {
			q.err = fault.New(fault.Invalid, "cursor ordering requires output fields")
			continue
		}
		node := order.projectionOrder().node
		field, ok := node.expression.(fieldRef)
		if !ok {
			q.err = fault.New(fault.Invalid, "cursor ordering requires output fields")
			continue
		}
		q.orders = append(q.orders, Order[CursorScope[R]]{field: field, descending: node.descending})
	}
	return q
}

// UniqueBy declares a nonempty combination of output fields identifying each
// result row under database equality, treating NULLs as equal. It replaces an
// earlier declaration; absent ordered key fields are appended ascending. This
// is an application assertion, not a DISTINCT operation or a schema constraint.
func (q CursorQuery[R]) UniqueBy(keys ...Group[CursorScope[R]]) CursorQuery[R] {
	q.unique = make([]fieldRef, len(keys))
	for i, key := range keys {
		field, ok := key.node.(fieldRef)
		if !ok {
			q.err = fault.New(fault.Invalid, "cursor identity requires output fields")
		}
		q.unique[i] = field
	}
	return q
}

func (q CursorQuery[R]) canonical() (CursorQuery[R], []RecordField[R], []bool, error) {
	fail := func(err error) (CursorQuery[R], []RecordField[R], []bool, error) {
		return CursorQuery[R]{}, nil, nil, err
	}
	if q.err != nil {
		return fail(q.err)
	}
	if q.metadata == nil || q.metadata.scan == nil || q.node.source.query == nil || len(q.unique) == 0 || len(q.unique) > MaxCursorFields || len(q.orders) > MaxCursorFields {
		return fail(fault.New(fault.Invalid, "cursor result requires metadata and a bounded UniqueBy declaration"))
	}
	columns := make(map[string]Column, len(q.metadata.columns))
	for _, column := range q.metadata.columns {
		columns[column.Name] = column
	}
	seen := make(map[string]bool, len(q.orders))
	valid := func(f fieldRef) bool { _, ok := columns[f.column]; return ok && f.table == q.node.source.name() }
	for _, order := range q.orders {
		if !valid(order.field) || seen[order.field.column] {
			return fail(fault.New(fault.Invalid, "cursor order uses an unknown or repeated output field"))
		}
		seen[order.field.column] = true
	}
	q.orders = slices.Clone(q.orders)
	keys := make(map[string]bool, len(q.unique))
	for _, key := range q.unique {
		if !valid(key) || keys[key.column] {
			return fail(fault.New(fault.Invalid, "cursor identity uses an unknown or repeated output field"))
		}
		keys[key.column] = true
		if !seen[key.column] {
			q.orders = append(q.orders, Order[CursorScope[R]]{field: key})
			seen[key.column] = true
		}
	}
	if len(q.orders) > MaxCursorFields || len(q.metadata.fields) > len(q.metadata.columns) {
		return fail(fault.New(fault.Invalid, "cursor result exceeds its field bound"))
	}
	getters := make(map[string]RecordField[R], len(q.metadata.fields))
	for _, field := range q.metadata.fields {
		if _, exists := getters[field.column]; exists || field.get == nil || field.decode == nil {
			return fail(fault.New(fault.Invalid, "cursor result has invalid field getters"))
		}
		if _, ok := columns[field.column]; !ok {
			return fail(fault.New(fault.Invalid, "cursor getter is not a declared output field"))
		}
		getters[field.column] = field
	}
	fields := make([]RecordField[R], len(q.orders))
	required := make([]bool, len(q.orders))
	for i, order := range q.orders {
		field, ok := getters[order.field.column]
		if !ok {
			return fail(fault.New(fault.Invalid, "cursor result requires generated codecs and getters for every ordered field"))
		}
		if field.sensitive {
			return fail(fault.New(fault.Invalid, "sensitive fields cannot be cursor keys"))
		}
		fields[i] = field
		required[i] = !columns[field.column].Nullable
	}
	q.node.orders = orderNodes(q.orders)
	return q, fields, required, nil
}

func (q CursorQuery[R]) reader() readResult[R] {
	r := readResult[R]{node: q.node, err: q.err}
	if q.metadata != nil {
		r.scan = q.metadata.scan
		r.lifecycle = q.metadata.lifecycle
	}
	return r
}

// Compile validates the declared cursor identity and compiles the canonical
// result order, without a page boundary or database execution.
func (q CursorQuery[R]) Compile() (Statement, error) {
	q, _, _, err := q.canonical()
	if err != nil {
		return Statement{}, err
	}
	return q.reader().Compile()
}

// Paginate reads size plus one complete results using the shared nullable,
// bidirectional keyset/token machinery. It returns no total and never queries
// again to construct navigation hints. Any failure discards the entire page.
func (q CursorQuery[R]) Paginate(ctx context.Context, executor database.Executor, request CursorRequest[R]) (CursorPage[R], error) {
	if err := executionContext(ctx, executor); err != nil {
		return CursorPage[R]{}, err
	}
	if !validCursorRequest(request) {
		return CursorPage[R]{}, cursorInputFailure(invalidCursor())
	}
	q, fields, required, err := q.canonical()
	if err != nil {
		return CursorPage[R]{}, err
	}
	statement, err := q.reader().Compile()
	if err != nil {
		return CursorPage[R]{}, err
	}
	identity := []string{"result-cursor"}
	for _, key := range q.unique {
		identity = append(identity, key.column)
	}
	scope, err := cursorStatementScope[R](statement, identity...)
	if err != nil {
		return CursorPage[R]{}, err
	}
	navigation, err := readCursorBoundary(request, scope, fields, required)
	if err != nil {
		return CursorPage[R]{}, err
	}
	orders := cursorOrders(q.orders, navigation.backward)
	q.node.orders = orderNodes(orders)
	if navigation.present {
		q.node.predicates = append(slices.Clone(q.node.predicates), cursorPredicate(orders, navigation.keys).expression)
	}
	q.node.limit = value.Set(request.Size + 1)
	items, err := q.reader().All(ctx, executor)
	if err != nil {
		return CursorPage[R]{}, err
	}
	items, more := trimPageLookahead(items, request.Size)
	return finishCursorPage(items, request.Size, more, navigation.backward, navigation.present, scope, fields)
}

// ValueCursorQuery adds a typed single-value expression and identity key to the
// result cursor builder. UniqueBy(Key()) asserts values are unique; use an
// explicit Distinct selection first when the original values can repeat.
type ValueCursorQuery[V any] struct {
	CursorQuery[V]
	codec codec.Codec[V]
}

// ValueCursorFor creates the same completed-result boundary for a scalar query
// or value set. It does not silently remove duplicate values.
func ValueCursorFor[V any](source ValueQuerySource[V]) ValueCursorQuery[V] {
	s := valueSource(source)
	return ValueCursorQuery[V]{CursorQuery: cursorForRecord(recordForValue(s)), codec: s.codec}
}

// Value retains the exact value type and codec for ordering the completed result.
func (q ValueCursorQuery[V]) Value() Expression[CursorScope[V], V] {
	return Expression[CursorScope[V], V]{node: fieldRef{q.node.source.name(), valueColumnName}, codec: q.codec}
}

// Key is the single output column, suitable for an explicit UniqueBy declaration.
func (q ValueCursorQuery[V]) Key() Group[CursorScope[V]] {
	return Group[CursorScope[V]]{node: fieldRef{q.node.source.name(), valueColumnName}}
}
