package rows

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TransactionSubquerySource retains a SELECT without permitting ordinary execution.
// Example: rows.TransactionExistsQuery(parent,child).
type TransactionSubquerySource interface{ transactionSubquery() ormcore.Source }

// TransactionSubquery explicitly lifts an ordinary SELECT for transaction composition.
// Example: rows.TransactionSubqueryOf(rows.From(Users.Query())).
type TransactionSubquery struct{ source ormcore.Source }

func (s TransactionSubquery) transactionSubquery() ormcore.Source { return s.source }

// TransactionSubqueryOf preserves all clauses of an ordinary SELECT.
// Example: rows.TransactionSubqueryOf(rows.FromRows(report)).
func TransactionSubqueryOf[S any](source SelectionSource[S]) TransactionSubquery {
	return TransactionSubquery{source: sourceOf(source)}
}
func transactionSubquery(source TransactionSubquerySource) ormcore.Source {
	if ormcore.NilDescriptor(source) {
		return ormcore.Source{Err: ormcore.Invalid("transaction subquery requires a source")}
	}
	s := source.transactionSubquery()
	s.Node.Source.RequiresTransaction = true
	return s
}
func transactionRecordSubquery(r ormcore.RecordSource) ormcore.Source {
	r.Source.Err = ormcore.ValidateRecordSource(r, r.Record.Type, true)
	return r.Source
}

// TransactionValueSource retains a single value's exact codec and executor requirement.
// Example: rows.TransactionScalarRowQuery(parent,selected).Value().
type TransactionValueSource[V any] interface {
	transactionValue(*V) (ormcore.RecordSource, ormcore.Column[V])
}

func (q TransactionValue[S, V]) transactionValue(*V) (ormcore.RecordSource, ormcore.Column[V]) {
	return q.core, q.column
}
func transactionValueSource[V any](q TransactionValueSource[V]) (ormcore.RecordSource, ormcore.Column[V]) {
	if ormcore.NilDescriptor(q) {
		return ormcore.RecordSource{Source: ormcore.Source{Err: ormcore.Invalid("transaction scalar requires a source")}}, ormcore.Column[V]{}
	}
	r, c := q.transactionValue(nil)
	r.Source.Err = ormcore.ValidateRecordSource(r, reflect.TypeFor[V](), true)
	r.Source.Node.Source.RequiresTransaction = true
	if c.Scan == nil || c.Bind == nil || c.Clone == nil {
		r.Source.Err = safeerror.Join("operation failed", r.Source.Err, ormcore.Invalid("transaction scalar requires its codec"))
	}
	return r, c
}

// TransactionScalar retains a selected value and optional locks for composition only.
// Example: rows.TransactionScalarOf(selected).ForUpdate().SkipLocked().
type TransactionScalar[S TransactionScopeType, V any] struct {
	_                      [0]*S
	_                      [0]*V
	transactionScalarPhase [0]struct{}
	core                   ormcore.RecordSource
	column                 ormcore.Column[V]
}

func (q TransactionScalar[S, V]) transactionValue(*V) (ormcore.RecordSource, ormcore.Column[V]) {
	return q.core, q.column
}
func (q TransactionScalar[S, V]) transactionSubquery() ormcore.Source {
	return transactionRecordSubquery(q.core)
}

// TransactionScalarOf retains codecs when adding locks to scalar subqueries.
// Example: rows.TransactionScalarOf(rows.SelectTransactionValue(source,expr.Row(id).Value())).ForShare().
func TransactionScalarOf[S TransactionScopeType, V any](q TransactionValue[S, V]) TransactionScalar[S, V] {
	r, c := transactionValueSource(q)
	return TransactionScalar[S, V]{core: r, column: c}
}

// TransactionExistsQuery anchors an independent SELECT to a transaction scope.
// Example: rows.TransactionExistsQuery(parent,selected).
func TransactionExistsQuery[S TransactionScopeType](outer TransactionSelectionSource[S], inner TransactionSubquerySource) expr.Predicate[S] {
	return expr.FoundryPredicateValue[S](ormcore.ExistsPredicate(transactionSelectionOf(outer), transactionSubquery(inner)), sqlowner.Expressions())
}

// TransactionInQuery requires an exact row/result type in a transaction scope.
// Example: rows.TransactionInQuery(id,selected).
func TransactionInQuery[S TransactionScopeType, V any](left expr.RowValue[S, V], inner TransactionValueSource[V]) expr.Predicate[S] {
	r, _ := transactionValueSource(inner)
	return expr.FoundryPredicateValue[S](ormcore.MembershipPredicate(expr.FoundryRowValue(left, sqlowner.Expressions()), r.Source), sqlowner.Expressions())
}

// TransactionInNullableQuery explicitly admits NULL inner values.
// Example: rows.TransactionInNullableQuery(id,nullableIDs).
func TransactionInNullableQuery[S TransactionScopeType, V any](left expr.RowValue[S, V], inner TransactionValueSource[value.Nullable[V]]) expr.Predicate[S] {
	r, _ := transactionValueSource(inner)
	return expr.FoundryPredicateValue[S](ormcore.MembershipPredicate(expr.FoundryRowValue(left, sqlowner.Expressions()), r.Source), sqlowner.Expressions())
}

// TransactionScalarRowQuery makes a transaction scalar available in row expressions.
// Example: rows.TransactionScalarRowQuery(parent,selected).
func TransactionScalarRowQuery[S TransactionScopeType, V any](outer TransactionSelectionSource[S], inner TransactionValueSource[V]) expr.RowExpression[S, value.Nullable[V]] {
	r, c := transactionValueSource(inner)
	v := expr.Nullable(expr.FoundrySelectedValue[S](ormcore.ScalarExpression(transactionSelectionOf(outer), r.Source), c, sqlowner.Expressions()))
	return expr.FoundryRowExpression[S](expr.FoundrySelected(v, sqlowner.Expressions()), expr.FoundrySelectedColumn(v, sqlowner.Expressions()), sqlowner.Expressions())
}

// TransactionScalarNullableRowQuery makes a transaction scalar available in row expressions.
// Example: rows.TransactionScalarNullableRowQuery(parent,selected).
func TransactionScalarNullableRowQuery[S TransactionScopeType, V any](outer TransactionSelectionSource[S], inner TransactionValueSource[value.Nullable[V]]) expr.RowExpression[S, value.Nullable[V]] {
	r, c := transactionValueSource(inner)
	v := expr.FoundrySelectedValue[S](ormcore.ScalarExpression(transactionSelectionOf(outer), r.Source), c, sqlowner.Expressions())
	return expr.FoundryRowExpression[S](expr.FoundrySelected(v, sqlowner.Expressions()), expr.FoundrySelectedColumn(v, sqlowner.Expressions()), sqlowner.Expressions())
}
func (q TransactionRead[S, R]) transactionSubquery() ormcore.Source {
	return transactionRecordSubquery(q.core)
}
func (q TransactionRecord[R]) transactionSubquery() ormcore.Source {
	return transactionRecordSubquery(q.core)
}
func (q LockedResult[S, R]) transactionSubquery() ormcore.Source {
	return transactionRecordSubquery(q.core)
}
func (q TransactionValue[S, V]) transactionSubquery() ormcore.Source {
	return transactionRecordSubquery(q.core)
}
func (q TransactionAliased[A, R]) transactionSubquery() ormcore.Source {
	return transactionSelectionOf[TransactionAlias[A, R]](q)
}
func (q TransactionInnerJoined[L, R]) transactionSubquery() ormcore.Source { return q.state.source }
func (q TransactionLeftJoined[L, R]) transactionSubquery() ormcore.Source  { return q.state.source }
func (q TransactionRightJoined[L, R]) transactionSubquery() ormcore.Source { return q.state.source }
func (q TransactionFullJoined[L, R]) transactionSubquery() ormcore.Source  { return q.state.source }
func (q TransactionCrossJoined[L, R]) transactionSubquery() ormcore.Source { return q.state.source }

// TransactionValueInput lifts an ordinary single-column source without execution.
// Example: rows.TransactionValueOf(rows.Values(selected)).
type TransactionValueInput[V any] struct {
	_                          [0]*V
	transactionValueInputPhase [0]struct{}
	core                       ormcore.RecordSource
	column                     ormcore.Column[V]
}

func (q TransactionValueInput[V]) transactionValue(*V) (ormcore.RecordSource, ormcore.Column[V]) {
	return q.core, q.column
}
func (q TransactionValueInput[V]) transactionSubquery() ormcore.Source {
	return transactionRecordSubquery(q.core)
}

// TransactionValueOf retains an ordinary scalar's codec, clauses and sensitivity.
// Example: rows.TransactionInQuery(id,rows.TransactionValueOf(rows.Values(selected))).
func TransactionValueOf[V any](source ValueSource[V]) TransactionValueInput[V] {
	r, c := valueSource(source)
	r.Transaction = true
	return TransactionValueInput[V]{core: r, column: c}
}
