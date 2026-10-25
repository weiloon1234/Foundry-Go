package query

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

type setOperator = querycore.SetOperator

const unionSet = querycore.UnionSet

const unionAllSet = querycore.UnionAllSet

const intersectSet = querycore.IntersectSet

const intersectAllSet = querycore.IntersectAllSet

const exceptSet = querycore.ExceptSet

const exceptAllSet = querycore.ExceptAllSet

type setNode = querycore.SetNode

const setAlias = "foundry_set"

// Set is the input scope of a combined result, separate from either input's
// scope. Use the result's Scope with generated model/projection FieldsAt methods.
type Set[R any] struct{ _ [0]*R }

// SetQuery combines complete records of one type. It is read-only and retains
// each input's filters, ordering and window before the set operation.
type SetQuery[R any] struct{ record recordQuery[R] }

func combineRecords[R any](left, right RecordQuerySource[R], operator setOperator) SetQuery[R] {
	if nilDescriptor(left) || nilDescriptor(right) {
		return SetQuery[R]{record: recordQuery[R]{err: safeerror.NewFault(fault.Invalid, "set operation requires two record queries")}}
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
		Source:     tableSource{Set: &setNode{Left: left.node, Right: right.node, Operator: operator}, Alias: setAlias, Columns: left.columns},
		Selections: columnSelections(setAlias, left.columns),
	}
	return result
}

func compatibleRecords[R any](left, right recordQuery[R]) error {
	for _, r := range []recordQuery[R]{left, right} {
		if r.err != nil {
			return r.err
		}
		if len(r.columns) == 0 || len(r.columns) > MaxExpressionNodes || len(r.columns) != len(r.node.Selections) || r.scan == nil {
			return safeerror.NewFault(fault.Invalid, "set input requires a complete record and decoder")
		}
	}
	if len(left.columns) != len(right.columns) {
		return safeerror.NewFault(fault.Invalid, "set input column counts differ")
	}
	for i, l := range left.columns {
		r := right.columns[i]
		if l.Name != r.Name || l.Nullable != r.Nullable {
			return safeerror.NewFault(fault.Invalid, "set input record layouts differ")
		}
	}
	return nil
}

// Scope exposes the combined record for generated typed field access.
func (q SetQuery[R]) Scope() RecordScope[Set[R], R] {
	if q.record.err != nil || q.record.node.Source.Set == nil {
		return RecordScope[Set[R], R]{}
	}
	return RecordScope[Set[R], R]{table: setAlias, record: q.record.metadata()}
}

// Where filters the combined result; input filters remain inside their inputs.
func (q SetQuery[R]) Where(predicates ...Predicate[Set[R]]) SetQuery[R] {
	q.record.node.Predicates = slices.Clone(q.record.node.Predicates)
	for _, p := range predicates {
		q.record.node.Predicates = append(q.record.node.Predicates, p.expression)
	}
	return q
}

// OrderBy orders the combined result, independently of each input's ordering.
func (q SetQuery[R]) OrderBy(orders ...ProjectionOrder[Set[R]]) SetQuery[R] {
	q.record.node.Orders = slices.Clone(q.record.node.Orders)
	for _, o := range orders {
		if nilDescriptor(o) {
			q.record.err = safeerror.NewFault(fault.Invalid, "set ordering requires an expression")
			continue
		}
		q.record.node.Orders = append(q.record.node.Orders, o.projectionOrder().node)
	}
	return q
}

// Limit bounds the combined result window.
func (q SetQuery[R]) Limit(count int) SetQuery[R] { q.record.node.Limit = value.Set(count); return q }

// Offset skips rows in the combined result window.
func (q SetQuery[R]) Offset(count int) SetQuery[R] { q.record.node.Offset = count; return q }

func (q SetQuery[R]) recordQuery() recordQuery[R] {
	r := q.record
	if r.err != nil {
		return r
	}
	if r.node.Source.Set == nil || r.scan == nil {
		r.err = safeerror.NewFault(fault.Invalid, "set query requires two complete inputs")
	}
	r.directSource = len(r.node.Predicates) == 0 && len(r.node.Orders) == 0 && !r.node.Limit.IsSet() && r.node.Offset == 0
	return r
}

func (q SetQuery[R]) subquery() subquery {
	r := q.recordQuery()
	return subquery{Node: r.node, Err: r.err}
}

func (q SetQuery[R]) projectionSource() projectionSource[Set[R]] {
	r := q.recordQuery()
	node := r.node
	node.Selections = nil
	return projectionSource[Set[R]]{node: node, err: r.err}
}

func (q SetQuery[R]) scopeSource() queryScope[Set[R]] { return scopeOf(q.projectionSource()) }
