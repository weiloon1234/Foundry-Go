package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

type subquery = querycore.Subquery

type subqueryPredicate = querycore.SubqueryPredicate

type scalarSubquery = querycore.ScalarSubquery

// SubquerySource is a validated SELECT boundary, independent of its output shape.
// Models, projections, value queries, aliases and join sources implement it.
type SubquerySource interface{ subquery() subquery }

func (q Query[M]) subquery() subquery {
	r := q.recordQuery()
	return subquery{Node: r.node, Err: r.err}
}

func (q ProjectionQuery[S, P]) subquery() subquery {
	node, err := q.selectNode()
	return subquery{Node: node, Err: err}
}

func (a AliasedSource[A, M]) subquery() subquery {
	node := a.input.node
	node.Selections = columnSelections(node.Source.Name(), node.Source.Columns)
	return subquery{Node: node, Err: a.input.err}
}

func (j joinedSource[S, L, R]) subquery() subquery {
	return subquery{Node: j.input.node, Err: j.input.err}
}

// InQuery compares compatible values from a single-column query. SQL NULL keeps
// its three-valued semantics, including when this predicate is negated.
func (f ScalarField[M, V]) InQuery(source ValueQuerySource[V]) Predicate[M] {
	return f.inSubquery(valueSource(source).subquery)
}

// InNullableQuery explicitly accepts a nullable result of the same base type.
// It does not remove NULL rows or replace them with Go zero values.
func (f ScalarField[M, V]) InNullableQuery(source ValueQuerySource[value.Nullable[V]]) Predicate[M] {
	return f.inSubquery(valueSource(source).subquery)
}

func (f ScalarField[M, V]) inSubquery(q subquery) Predicate[M] {
	return Predicate[M]{expression: retainPredicate(subqueryPredicate{Query: q, Operand: f.rowValue().node})}
}

// ExistsQuery is an uncorrelated existence predicate. Outer anchors the result
// scope; its filters remain the responsibility of the outer query. Inner clauses,
// grouping and windows retain their ordinary SELECT semantics.
func ExistsQuery[S any](outer ProjectionSource[S], inner SubquerySource) Predicate[S] {
	q := subquery{}
	if nilDescriptor(inner) {
		q.Err = fault.New(fault.Invalid, "EXISTS requires a query")
	} else {
		q = inner.subquery()
	}
	if err := outerScopeError(outer); err != nil {
		q.Err = err
	}
	return Predicate[S]{expression: retainPredicate(subqueryPredicate{Query: q})}
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
		q.subquery.Err = outerError
	}
	if value.IsNullableType[V]() {
		q.subquery.Err = fault.New(fault.Invalid, "nullable scalar inputs require ScalarNullableQuery or ScalarNullableRowQuery")
	}
	return Expression[S, value.Nullable[V]]{node: scalarSubquery{Query: q.subquery}, codec: codec.Nullable(q.codec)}
}

// ScalarNullableQuery preserves a single nullable layer for both empty results
// and an inner SQL NULL. A non-null inner value retains its concrete codec.
func ScalarNullableQuery[S, V any](outer ProjectionSource[S], inner ValueQuerySource[value.Nullable[V]]) Expression[S, value.Nullable[V]] {
	return scalarNullableQuery[S](inner, outerScopeError(outer))
}

func scalarNullableQuery[S, V any](inner ValueQuerySource[value.Nullable[V]], outerError error) Expression[S, value.Nullable[V]] {
	q := valueSource(inner)
	if outerError != nil {
		q.subquery.Err = outerError
	}
	return Expression[S, value.Nullable[V]]{node: scalarSubquery{Query: q.subquery}, codec: q.codec}
}

func valueSource[V any](source ValueQuerySource[V]) valueSubquery[V] {
	if nilDescriptor(source) {
		return valueSubquery[V]{subquery: subquery{Err: fault.New(fault.Invalid, "subquery requires a single-column value query")}}
	}
	return source.valueQuery()
}

func outerScopeError[S any](source ProjectionSource[S]) error {
	if nilDescriptor(source) {
		return fault.New(fault.Invalid, "subquery expression requires an outer source scope")
	}
	return source.projectionSource().err
}
