package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// SubquerySource is a validated SELECT boundary, independent of its output shape.
// Models, projections, value queries, aliases and join sources implement it.
type SubquerySource interface{ subquery() subquery }
type subquery struct {
	node        selectNode
	err         error
	correlation *scopeRequirement
}

func (q Query[M]) subquery() subquery {
	r := q.recordQuery()
	return subquery{node: r.node, err: r.err}
}
func (q ProjectionQuery[S, P]) subquery() subquery {
	node, err := q.selectNode()
	return subquery{node: node, err: err}
}
func (a AliasedSource[A, M]) subquery() subquery {
	node := a.input.node
	node.selections = columnSelections(node.source.name(), node.source.columns)
	return subquery{node: node, err: a.input.err}
}
func (j joinedSource[S, L, R]) subquery() subquery {
	return subquery{node: j.input.node, err: j.input.err}
}

type subqueryPredicate struct {
	query   subquery
	operand valueExpression // nil means EXISTS; otherwise single-column IN.
}

func (subqueryPredicate) expressionNode() {}

type scalarSubquery struct{ query subquery }

func (scalarSubquery) valueNode() {}

// InQuery compares compatible values from a single-column query. SQL NULL keeps
// its three-valued semantics, including when this predicate is negated.
func (f valueField[M, V]) InQuery(source ValueQuerySource[V]) Predicate[M] {
	return f.inSubquery(valueSource(source).subquery)
}

// InNullableQuery explicitly accepts a nullable result of the same base type.
// It does not remove NULL rows or replace them with Go zero values.
func (f valueField[M, V]) InNullableQuery(source ValueQuerySource[value.Nullable[V]]) Predicate[M] {
	return f.inSubquery(valueSource(source).subquery)
}
func (f valueField[M, V]) inSubquery(q subquery) Predicate[M] {
	return Predicate[M]{expression: subqueryPredicate{query: q, operand: f.ref}}
}

// ExistsQuery is an uncorrelated existence predicate. Outer anchors the result
// scope; its filters remain the responsibility of the outer query. Inner clauses,
// grouping and windows retain their ordinary SELECT semantics.
func ExistsQuery[S any](outer ProjectionSource[S], inner SubquerySource) Predicate[S] {
	q := subquery{}
	if nilDescriptor(inner) {
		q.err = fault.New(fault.Invalid, "EXISTS requires a query")
	} else {
		q = inner.subquery()
	}
	if err := outerScopeError(outer); err != nil {
		q.err = err
	}
	return Predicate[S]{expression: subqueryPredicate{query: q}}
}

// ScalarQuery selects an uncorrelated single value into the outer scope. No row
// becomes NULL; multiple rows return a database cardinality error. It never adds
// an implicit LIMIT. Use ScalarNullableQuery for an already nullable input.
func ScalarQuery[S, V any](outer ProjectionSource[S], inner ValueQuerySource[V]) Expression[S, value.Nullable[V]] {
	return scalarQuery[S](inner, outerScopeError(outer))
}

func scalarQuery[S, V any](inner ValueQuerySource[V], outerError error) Expression[S, value.Nullable[V]] {
	q := valueSource(inner)
	if outerError != nil {
		q.err = outerError
	}
	if value.IsNullableType[V]() {
		q.err = fault.New(fault.Invalid, "nullable scalar inputs require ScalarNullableQuery or ScalarNullableRowQuery")
	}
	return Expression[S, value.Nullable[V]]{node: scalarSubquery{query: q.subquery}, codec: codec.Nullable(q.codec)}
}

// ScalarNullableQuery preserves a single nullable layer for both empty results
// and an inner SQL NULL. A non-null inner value retains its concrete codec.
func ScalarNullableQuery[S, V any](outer ProjectionSource[S], inner ValueQuerySource[value.Nullable[V]]) Expression[S, value.Nullable[V]] {
	return scalarNullableQuery[S](inner, outerScopeError(outer))
}

func scalarNullableQuery[S, V any](inner ValueQuerySource[value.Nullable[V]], outerError error) Expression[S, value.Nullable[V]] {
	q := valueSource(inner)
	if outerError != nil {
		q.err = outerError
	}
	return Expression[S, value.Nullable[V]]{node: scalarSubquery{query: q.subquery}, codec: q.codec}
}
func valueSource[V any](source ValueQuerySource[V]) valueSubquery[V] {
	if nilDescriptor(source) {
		return valueSubquery[V]{subquery: subquery{err: fault.New(fault.Invalid, "subquery requires a single-column value query")}}
	}
	return source.valueQuery()
}
func outerScopeError[S any](source ProjectionSource[S]) error {
	if nilDescriptor(source) {
		return fault.New(fault.Invalid, "subquery expression requires an outer source scope")
	}
	return source.projectionSource().err
}

func (c *compiler) subquerySQL(q subquery, single bool, grouped map[fieldRef]bool, grouping bool) (string, error) {
	if q.err != nil {
		return "", q.err
	}
	if single && len(q.node.selections) != 1 {
		return "", fault.New(fault.Invalid, "value subquery must select exactly one column")
	}
	outer, err := c.correlationScope(q.correlation, grouped, grouping)
	if err != nil {
		return "", err
	}
	return c.selectSQLWithOuter(q.node, outer)
}
func (c *compiler) subqueryPredicate(p subqueryPredicate, grouped map[fieldRef]bool, grouping bool) (string, error) {
	var operand string
	if p.operand != nil {
		var err error
		operand, err = c.selectedExpression(p.operand, grouped, grouping)
		if err != nil {
			return "", err
		}
	}
	text, err := c.subquerySQL(p.query, p.operand != nil, grouped, grouping)
	if err != nil {
		return "", err
	}
	if p.operand == nil {
		return "EXISTS(" + text + ")", nil
	}
	return "(" + operand + " IN (" + text + "))", nil
}
