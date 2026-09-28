package query

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type setOperator uint8

const (
	unionSet setOperator = iota
	unionAllSet
	intersectSet
	intersectAllSet
	exceptSet
	exceptAllSet
)

const setAlias = "foundry_set"

type setNode struct {
	left, right selectNode
	operator    setOperator
}

// Set is the input scope of a combined result, separate from either input's
// scope. Use the result's Scope with generated model/projection FieldsAt methods.
type Set[R any] struct{ _ [0]*R }

// SetQuery combines complete records of one type. It is read-only and retains
// each input's filters, ordering and window before the set operation.
type SetQuery[R any] struct{ record recordQuery[R] }

func combineRecords[R any](left, right RecordQuerySource[R], operator setOperator) SetQuery[R] {
	if nilDescriptor(left) || nilDescriptor(right) {
		return SetQuery[R]{record: recordQuery[R]{err: fault.New(fault.Invalid, "set operation requires two record queries")}}
	}
	return combineRecordQueries(left.recordQuery(), right.recordQuery(), operator)
}
func combineRecordQueries[R any](left, right recordQuery[R], operator setOperator) SetQuery[R] {
	result := SetQuery[R]{record: recordQuery[R]{columns: left.columns, scan: left.scan, fields: left.fields, lifecycle: left.lifecycle}}
	if err := compatibleRecords(left, right); err != nil {
		result.record.err = err
		return result
	}
	result.record.node = selectNode{
		source:     tableSource{set: &setNode{left: left.node, right: right.node, operator: operator}, alias: setAlias, columns: left.columns},
		selections: columnSelections(setAlias, left.columns),
	}
	return result
}

func compatibleRecords[R any](left, right recordQuery[R]) error {
	for _, r := range []recordQuery[R]{left, right} {
		if r.err != nil {
			return r.err
		}
		if len(r.columns) == 0 || len(r.columns) > MaxExpressionNodes || len(r.columns) != len(r.node.selections) || r.scan == nil {
			return fault.New(fault.Invalid, "set input requires a complete record and decoder")
		}
	}
	if len(left.columns) != len(right.columns) {
		return fault.New(fault.Invalid, "set input column counts differ")
	}
	for i, l := range left.columns {
		r := right.columns[i]
		if l.Name != r.Name || l.Nullable != r.Nullable {
			return fault.New(fault.Invalid, "set input record layouts differ")
		}
	}
	return nil
}

// Scope exposes the combined record for generated typed field access.
func (q SetQuery[R]) Scope() RecordScope[Set[R], R] {
	if q.record.err != nil || q.record.node.source.set == nil {
		return RecordScope[Set[R], R]{}
	}
	return RecordScope[Set[R], R]{table: setAlias, record: q.record.metadata()}
}

// Where filters the combined result; input filters remain inside their inputs.
func (q SetQuery[R]) Where(predicates ...Predicate[Set[R]]) SetQuery[R] {
	q.record.node.predicates = slices.Clone(q.record.node.predicates)
	for _, p := range predicates {
		q.record.node.predicates = append(q.record.node.predicates, p.expression)
	}
	return q
}

// OrderBy orders the combined result, independently of each input's ordering.
func (q SetQuery[R]) OrderBy(orders ...ProjectionOrder[Set[R]]) SetQuery[R] {
	q.record.node.orders = slices.Clone(q.record.node.orders)
	for _, o := range orders {
		if nilDescriptor(o) {
			q.record.err = fault.New(fault.Invalid, "set ordering requires an expression")
			continue
		}
		q.record.node.orders = append(q.record.node.orders, o.projectionOrder().node)
	}
	return q
}

// Limit bounds the combined result window.
func (q SetQuery[R]) Limit(count int) SetQuery[R] { q.record.node.limit = value.Set(count); return q }

// Offset skips rows in the combined result window.
func (q SetQuery[R]) Offset(count int) SetQuery[R] { q.record.node.offset = count; return q }
func (q SetQuery[R]) recordQuery() recordQuery[R] {
	r := q.record
	if r.err != nil {
		return r
	}
	if r.node.source.set == nil || r.scan == nil {
		r.err = fault.New(fault.Invalid, "set query requires two complete inputs")
	}
	r.directSource = len(r.node.predicates) == 0 && len(r.node.orders) == 0 && !r.node.limit.IsSet() && r.node.offset == 0
	return r
}
func (q SetQuery[R]) subquery() subquery {
	r := q.recordQuery()
	return subquery{node: r.node, err: r.err}
}
func (q SetQuery[R]) projectionSource() projectionSource[Set[R]] {
	r := q.recordQuery()
	node := r.node
	node.selections = nil
	return projectionSource[Set[R]]{node: node, err: r.err}
}

func (q SetQuery[R]) scopeSource() queryScope[Set[R]] { return scopeOf(q.projectionSource()) }

func (c *compiler) setSQL(s setNode, columns []Column) (string, error) {
	if s.operator > exceptAllSet || len(columns) == 0 || len(columns) != len(s.left.selections) || len(columns) != len(s.right.selections) {
		return "", fault.New(fault.Invalid, "invalid set operator or output layout")
	}
	// Explicit output aliases make model columns and declared projection columns
	// interchangeable only after the typed record boundary has matched layouts.
	left := s.left
	left.selections = slices.Clone(left.selections)
	for i, column := range columns {
		left.selections[i].alias = column.Name
	}
	a, err := c.selectSQL(left)
	if err != nil {
		return "", err
	}
	b, err := c.selectSQL(s.right)
	if err != nil {
		return "", err
	}
	operator := [...]string{"UNION", "UNION ALL", "INTERSECT", "INTERSECT ALL", "EXCEPT", "EXCEPT ALL"}[s.operator]
	return "(" + a + ") " + operator + " (" + b + ")", nil
}
