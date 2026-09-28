package query

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TransactionSubquerySource carries a SELECT whose consumer must require Tx.
type TransactionSubquerySource interface{ transactionSubquery() subquery }

// TransactionSubquery explicitly lifts an ordinary SELECT into transaction composition.
type TransactionSubquery struct{ query subquery }

// TransactionSubqueryOf retains an ordinary SELECT and its result window.
func TransactionSubqueryOf(source SubquerySource) TransactionSubquery {
	q := subquery{}
	if nilDescriptor(source) {
		q.err = fault.New(fault.Invalid, "transaction subquery requires a source")
	} else {
		q = source.subquery()
	}
	return TransactionSubquery{query: q}
}
func (q TransactionSubquery) transactionSubquery() subquery    { return q.query }
func (q TransactionQuery[S, R]) transactionSubquery() subquery { return q.query.subquery() }
func (q LockedQuery[M]) transactionSubquery() subquery {
	r := q.transactionRecord()
	return subquery{node: r.node, err: r.err}
}
func (q LockedResult[S, R]) transactionSubquery() subquery {
	r := q.transactionRecord()
	return subquery{node: r.node, err: r.err}
}
func (q TransactionRecord[R]) transactionSubquery() subquery {
	return subquery{node: q.record.node, err: q.record.err}
}
func (a TransactionAliasedSource[A, R]) transactionSubquery() subquery {
	node := a.input.node
	node.selections = columnSelections(node.source.name(), node.source.columns)
	return subquery{node: node, err: a.input.err}
}
func (j transactionJoinedSource[S, L, R]) transactionSubquery() subquery {
	return subquery{node: j.joined.input.node, err: j.joined.input.err}
}

type transactionSubqueryAdapter struct{ query subquery }

func (a transactionSubqueryAdapter) subquery() subquery { return a.query }
func transactionSubqueryOf(source TransactionSubquerySource) subquery {
	if nilDescriptor(source) {
		return subquery{err: fault.New(fault.Invalid, "transaction subquery requires a source")}
	}
	return source.transactionSubquery()
}

// TransactionExistsQuery anchors an independent transaction SELECT in its outer scope.
func TransactionExistsQuery[S TransactionScope](outer TransactionProjectionSource[S], inner TransactionSubquerySource) Predicate[S] {
	return ExistsQuery(transactionProjection(outer), transactionSubqueryAdapter{query: transactionSubqueryOf(inner)})
}

// TransactionScalarQuery retains locks and exact codecs. No row becomes NULL;
// multiple rows remain a database cardinality error. No LIMIT is added.
func TransactionScalarQuery[S TransactionScope, V any](outer TransactionProjectionSource[S], inner TransactionValueSource[V]) Expression[S, value.Nullable[V]] {
	return ScalarQuery(transactionProjection(outer), transactionValueAdapter[V]{value: transactionValueOf(inner)})
}

// TransactionScalarNullableQuery retains one nullable layer for an already nullable input.
func TransactionScalarNullableQuery[S TransactionScope, V any](outer TransactionProjectionSource[S], inner TransactionValueSource[value.Nullable[V]]) Expression[S, value.Nullable[V]] {
	return ScalarNullableQuery(transactionProjection(outer), transactionValueAdapter[value.Nullable[V]]{value: transactionValueOf(inner)})
}

// TransactionScalarRowQuery evaluates an independent locked SELECT in a row expression.
func TransactionScalarRowQuery[S TransactionScope, V any](outer TransactionProjectionSource[S], inner TransactionValueSource[V]) RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{TransactionScalarQuery(outer, inner)}
}

// TransactionScalarNullableRowQuery preserves nullable values and row ownership.
func TransactionScalarNullableRowQuery[S TransactionScope, V any](outer TransactionProjectionSource[S], inner TransactionValueSource[value.Nullable[V]]) RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{TransactionScalarNullableQuery(outer, inner)}
}

// TransactionInQuery compares compatible row values with a transaction-required
// single-column SELECT. Computed operands retain their own bindings and scope.
func TransactionInQuery[S TransactionScope, V any](left RowValue[S, V], inner TransactionValueSource[V]) Predicate[S] {
	return transactionMembership(left, transactionValueOf(inner).subquery)
}

// TransactionInNullableQuery explicitly admits NULL inner values. SQL's
// three-valued membership semantics are preserved, including under negation.
func TransactionInNullableQuery[S TransactionScope, V any](left RowValue[S, V], inner TransactionValueSource[value.Nullable[V]]) Predicate[S] {
	return transactionMembership(left, transactionValueOf(inner).subquery)
}
func transactionMembership[S TransactionScope, V any](left RowValue[S, V], inner subquery) Predicate[S] {
	operand := rowInput(left).Value().node
	if operand == nil {
		inner.err = fault.New(fault.Invalid, "transaction membership requires a row operand")
	}
	return Predicate[S]{expression: subqueryPredicate{query: inner, operand: operand}}
}
